package plugin

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent/pluginkv"
	"github.com/Entropy-Increase-Team/skinloom/ent/schema"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
)

// CronSchedule represents a parsed 5-field cron schedule (minute hour dom month dow).
type CronSchedule struct {
	minutes map[int]bool // 0-59
	hours   map[int]bool // 0-23
	dom     map[int]bool // 1-31
	months  map[int]bool // 1-12
	dow     map[int]bool // 0-6 (Sunday=0, also accepts 7->0)
	domStar bool
	dowStar bool
}

// ParseCron parses standard 5-field cron expressions and common @macros.
func ParseCron(expr string) (CronSchedule, error) {
	expr = strings.TrimSpace(expr)
	switch expr {
	case "@yearly", "@annually":
		expr = "0 0 1 1 *"
	case "@monthly":
		expr = "0 0 1 * *"
	case "@weekly":
		expr = "0 0 * * 0"
	case "@daily", "@midnight":
		expr = "0 0 * * *"
	case "@hourly":
		expr = "0 * * * *"
	}
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return CronSchedule{}, fmt.Errorf("cron expression must have 5 fields (got %d in %q)", len(fields), expr)
	}
	mins, err := parseCronField(fields[0], 0, 59, false)
	if err != nil {
		return CronSchedule{}, fmt.Errorf("cron minute %q: %w", fields[0], err)
	}
	hrs, err := parseCronField(fields[1], 0, 23, false)
	if err != nil {
		return CronSchedule{}, fmt.Errorf("cron hour %q: %w", fields[1], err)
	}
	dom, err := parseCronField(fields[2], 1, 31, false)
	if err != nil {
		return CronSchedule{}, fmt.Errorf("cron day-of-month %q: %w", fields[2], err)
	}
	mon, err := parseCronField(fields[3], 1, 12, false)
	if err != nil {
		return CronSchedule{}, fmt.Errorf("cron month %q: %w", fields[3], err)
	}
	dow, err := parseCronField(fields[4], 0, 7, true)
	if err != nil {
		return CronSchedule{}, fmt.Errorf("cron day-of-week %q: %w", fields[4], err)
	}
	return CronSchedule{
		minutes: mins,
		hours:   hrs,
		dom:     dom,
		months:  mon,
		dow:     dow,
		domStar: fields[2] == "*",
		dowStar: fields[4] == "*",
	}, nil
}

func parseCronField(spec string, minVal, maxVal int, wrapSunday bool) (map[int]bool, error) {
	out := make(map[int]bool)
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, errors.New("empty field item")
		}
		step := 1
		rangePart := part
		if base, stepStr, hasStep := strings.Cut(part, "/"); hasStep {
			n, err := strconv.Atoi(stepStr)
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("invalid step %q", stepStr)
			}
			step = n
			rangePart = base
		}
		lo, hi := minVal, maxVal
		if rangePart != "*" {
			if a, b, isRange := strings.Cut(rangePart, "-"); isRange {
				v1, err1 := strconv.Atoi(a)
				v2, err2 := strconv.Atoi(b)
				if err1 != nil || err2 != nil || v1 < minVal || v2 > maxVal || v1 > v2 {
					return nil, fmt.Errorf("invalid range %q", rangePart)
				}
				lo, hi = v1, v2
			} else {
				v, err := strconv.Atoi(rangePart)
				if err != nil || v < minVal || v > maxVal {
					return nil, fmt.Errorf("out of range %d..%d: %q", minVal, maxVal, rangePart)
				}
				if hasStep := strings.Contains(part, "/"); hasStep {
					lo = v
				} else {
					lo, hi = v, v
				}
			}
		}
		for v := lo; v <= hi; v += step {
			nv := v
			if wrapSunday && nv == 7 {
				nv = 0
			}
			out[nv] = true
		}
	}
	return out, nil
}

// Matches reports whether t (truncated to minute) satisfies the schedule.
func (c CronSchedule) Matches(t time.Time) bool {
	if !c.minutes[t.Minute()] || !c.hours[t.Hour()] || !c.months[int(t.Month())] {
		return false
	}
	domMatch := c.dom[t.Day()]
	dowMatch := c.dow[int(t.Weekday())]
	// Standard Vixie cron semantics: when both dom and dow are restricted (non-*),
	// either match suffices; otherwise both restrictions must hold.
	if !c.domStar && !c.dowStar {
		return domMatch || dowMatch
	}
	return domMatch && dowMatch
}

func (m *Manager) startCronLoop() {
	m.cronMu.Lock()
	if m.cronStop != nil {
		m.cronMu.Unlock()
		return
	}
	stop := make(chan struct{})
	wg := &sync.WaitGroup{}
	m.cronStop = stop
	m.cronWg = wg
	cronCtx, cronCancel := context.WithCancel(context.Background())
	wg.Add(1)
	m.cronMu.Unlock()

	go func() {
		defer wg.Done()
		defer cronCancel()
		go func() {
			<-stop
			cronCancel()
		}()
		now := time.Now().UTC()
		nextMinute := now.Truncate(time.Minute).Add(time.Minute)
		timer := time.NewTimer(time.Until(nextMinute))
		defer timer.Stop()

		for {
			select {
			case <-stop:
				return
			case tickAt := <-timer.C:
				m.runCronTick(cronCtx, wg, tickAt.UTC().Truncate(time.Minute))
				nextMinute = tickAt.UTC().Truncate(time.Minute).Add(time.Minute)
				timer.Reset(max(100*time.Millisecond, time.Until(nextMinute)))
			}
		}
	}()
}

func (m *Manager) stopCronLoop() {
	m.cronMu.Lock()
	stop := m.cronStop
	wg := m.cronWg
	m.cronStop = nil
	m.cronWg = nil
	m.cronMu.Unlock()
	if stop != nil {
		close(stop)
		if wg != nil {
			wg.Wait()
		}
	}
}

func (m *Manager) runCronTick(cronCtx context.Context, wg *sync.WaitGroup, minute time.Time) {
	if m.db != nil && minute.Minute() == 0 {
		// Hourly best-effort sweep of expired plugin_kv rows (including cron leases
		// and plugin TTL keys) so plugin_kv does not grow unboundedly.
		sweepCtx, cancel := context.WithTimeout(cronCtx, 5*time.Second)
		_, _ = m.db.PluginKV.Delete().
			Where(pluginkv.ExpiresAtNotNil(), pluginkv.ExpiresAtLT(time.Now().UTC())).
			Exec(sweepCtx)
		cancel()
	}

	m.mu.RLock()
	runtimes := make([]*pluginRuntime, 0, len(m.active))
	for _, rt := range m.active {
		if !rt.closed.Load() {
			runtimes = append(runtimes, rt)
		}
	}
	m.mu.RUnlock()

	slotKey := minute.Format("200601021504")
	for _, rt := range runtimes {
		rt.setupMu.Lock()
		jobs := append([]cronBinding(nil), rt.crons...)
		rt.setupMu.Unlock()

		for _, jb := range jobs {
			if !jb.Schedule.Matches(minute) {
				continue
			}
			if !m.claimCronSlot(cronCtx, rt.id, jb.Name, slotKey) {
				continue
			}
			runtimeRef := rt
			binding := jb
			wg.Add(1)
			go func() {
				defer wg.Done()
				if err := runtimeRef.invokeCron(cronCtx, resolveHandlerID(binding.HandlerID, binding.FnID), binding.Name); err != nil && !errors.Is(err, context.Canceled) {
					runtimeRef.log.Warn("plugin cron job failed", "job", binding.Name, "err", err)
				}
			}()
		}
	}
}

const cronKVPlugin = "__cron"

func cronLeaseKey(pluginID, jobName, slot string) string {
	key := pluginID + ":" + jobName + ":" + slot
	if len(key) <= 255 {
		return key
	}
	sum := sha256.Sum256([]byte(jobName))
	return pluginID + ":" + hex.EncodeToString(sum[:8]) + ":" + slot
}

// claimCronSlot acquires a unique lease row in plugin_kv under system-reserved
// plugin "__cron" so at most one worker across a cluster executes the slot.
func (m *Manager) claimCronSlot(ctx context.Context, pluginID, jobName, slot string) bool {
	if m.db == nil {
		return true
	}
	claimCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	exp := time.Now().UTC().Add(48 * time.Hour)
	raw, _ := json.Marshal(slot)
	err := m.db.PluginKV.Create().
		SetPlugin(cronKVPlugin).
		SetKey(cronLeaseKey(pluginID, jobName, slot)).
		SetValue(schema.KVValue(raw)).
		SetExpiresAt(exp).
		Exec(claimCtx)
	if err == nil {
		return true
	}
	if !db.IsConstraint(err) && m.log != nil {
		m.log.Warn("cron lease claim failed", "plugin", pluginID, "job", jobName, "err", err)
	}
	return false
}

// TriggerCron immediately runs a scheduled cron job on an enabled plugin.
func (m *Manager) TriggerCron(ctx context.Context, pluginID, jobName string) error {
	m.mu.RLock()
	rt := m.active[pluginID]
	m.mu.RUnlock()
	if rt == nil || rt.closed.Load() {
		return fmt.Errorf("plugin %q is not active", pluginID)
	}
	rt.setupMu.Lock()
	var found *cronBinding
	for idx := range rt.crons {
		if rt.crons[idx].Name == jobName {
			cp := rt.crons[idx]
			found = &cp
			break
		}
	}
	rt.setupMu.Unlock()
	if found == nil {
		return fmt.Errorf("cron job %q not registered on plugin %q", jobName, pluginID)
	}
	return rt.invokeCron(ctx, resolveHandlerID(found.HandlerID, found.FnID), found.Name)
}
