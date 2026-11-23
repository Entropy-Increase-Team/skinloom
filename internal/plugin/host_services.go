package plugin

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

type cronBinding struct {
	Name      string
	Expr      string
	Schedule  CronSchedule
	FnID      int
	HandlerID string
}

type serviceBinding struct {
	Name      string
	FnID      int
	HandlerID string
}

func (r *pluginRuntime) hostCron(_ context.Context, args []any) (any, error) {
	if err := r.perms.Check(PermCron); err != nil {
		return nil, err
	}
	op, _ := argAt(args, 0).(string)
	switch op {
	case "schedule":
		name := strings.TrimSpace(fmt.Sprint(argAt(args, 1)))
		expr := strings.TrimSpace(fmt.Sprint(argAt(args, 2)))
		fnID := int(asFloat(argAt(args, 3)))
		if name == "" || name == "<nil>" {
			return nil, errors.New("cron job name is required")
		}
		sched, err := ParseCron(expr)
		if err != nil {
			return nil, err
		}
		r.setupMu.Lock()
		if r.collecting {
			r.crons = append(r.crons, cronBinding{
				Name:     name,
				Expr:     expr,
				Schedule: sched,
				FnID:     fnID,
			})
		}
		r.setupMu.Unlock()
		return nil, nil
	default:
		return nil, fmt.Errorf("unknown cron op %q", op)
	}
}

func (r *pluginRuntime) hostServices(ctx context.Context, args []any) (any, error) {
	op, _ := argAt(args, 0).(string)
	switch op {
	case "expose":
		name := strings.TrimSpace(fmt.Sprint(argAt(args, 1)))
		fnID := int(asFloat(argAt(args, 2)))
		if name == "" || name == "<nil>" {
			return nil, errors.New("service name is required")
		}
		if !containsString(r.manifest.Services, name) {
			return nil, fmt.Errorf("%w: service %q is not declared in plugin.json services", ErrPermissionDenied, name)
		}
		r.setupMu.Lock()
		if r.collecting {
			replaced := false
			for i := range r.services {
				if r.services[i].Name == name {
					r.services[i].FnID = fnID
					replaced = true
					break
				}
			}
			if !replaced {
				r.services = append(r.services, serviceBinding{Name: name, FnID: fnID})
			}
		}
		r.setupMu.Unlock()
		return nil, nil

	case "call":
		targetID := strings.TrimSpace(fmt.Sprint(argAt(args, 1)))
		svcName := strings.TrimSpace(fmt.Sprint(argAt(args, 2)))
		if targetID == "" || targetID == "<nil>" || svcName == "" || svcName == "<nil>" {
			return nil, errors.New("services.use requires (pluginId, serviceName)")
		}
		if r.callService == nil {
			return nil, errors.New("service bus unavailable")
		}
		var callArgs []any
		if rawArgs, ok := argAt(args, 3).([]any); ok {
			callArgs = rawArgs
		}
		return r.callService(ctx, targetID, svcName, callArgs...)

	case "discover":
		svcName := strings.TrimSpace(fmt.Sprint(argAt(args, 1)))
		if svcName == "" || svcName == "<nil>" {
			return nil, errors.New("services.discover requires serviceName")
		}
		if r.discoverServices == nil {
			return []string{}, nil
		}
		return r.discoverServices(svcName)

	default:
		return nil, fmt.Errorf("unknown services op %q", op)
	}
}

func containsString(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}
