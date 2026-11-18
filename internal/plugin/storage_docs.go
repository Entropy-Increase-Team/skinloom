package plugin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Entropy-Increase-Team/skinloom/ent"
	"github.com/Entropy-Increase-Team/skinloom/ent/plugindoc"
	"github.com/Entropy-Increase-Team/skinloom/ent/predicate"
	"github.com/Entropy-Increase-Team/skinloom/internal/platform/db"
)

var (
	// ErrUnknownCollection means the collection was not declared in plugin.json.
	ErrUnknownCollection = errors.New("unknown plugin collection")
	// ErrUniqueConstraint means a string:unique indexed field collided with an existing doc.
	ErrUniqueConstraint = errors.New("unique constraint violation")
	// ErrQuotaExceeded means a plugin storage quota limit was reached.
	ErrQuotaExceeded = errors.New("plugin storage quota exceeded")
)

// UnindexedFieldError explains why a query/sort failed so plugin authors know to
// declare the field under storage.collections.<name>.indexes in plugin.json.
type UnindexedFieldError struct {
	Collection string
	Field      string
}

func (e *UnindexedFieldError) Error() string {
	return fmt.Sprintf("unindexed field %q in collection %q; declare it under storage.collections.%s.indexes in plugin.json",
		e.Field, e.Collection, e.Collection)
}

type slotKind int

const (
	slotString slotKind = iota
	slotUniqueString
	slotNumber
	slotTime
)

type slotBinding struct {
	field  string
	column string // u1, u2, s1..s3, n1..n3, t1..t2
	kind   slotKind
}

type compiledCollection struct {
	name    string
	byField map[string]slotBinding
	slots   []slotBinding
}

func compileCollectionSlots(name string, spec CollectionSpec) (*compiledCollection, error) {
	fields := make([]string, 0, len(spec.Indexes))
	for f := range spec.Indexes {
		if !indexFieldRe.MatchString(f) {
			return nil, fmt.Errorf("%w: invalid index field name %q in collection %q", ErrBadManifest, f, name)
		}
		fields = append(fields, f)
	}
	// Deterministic sorted assignment guarantees slots never shift across restarts/replicas.
	sort.Strings(fields)

	var uIdx, sIdx, nIdx, tIdx int
	cc := &compiledCollection{
		name:    name,
		byField: make(map[string]slotBinding, len(fields)),
		slots:   make([]slotBinding, 0, len(fields)),
	}
	for _, f := range fields {
		typ := spec.Indexes[f]
		var sb slotBinding
		sb.field = f
		switch typ {
		case "string:unique":
			if uIdx >= 2 {
				return nil, fmt.Errorf("%w: collection %q exceeds max 2 'string:unique' index slots", ErrBadManifest, name)
			}
			sb.kind = slotUniqueString
			sb.column = fmt.Sprintf("u%d", uIdx+1)
			uIdx++
		case "string":
			if sIdx >= 3 {
				return nil, fmt.Errorf("%w: collection %q exceeds max 3 'string' index slots", ErrBadManifest, name)
			}
			sb.kind = slotString
			sb.column = fmt.Sprintf("s%d", sIdx+1)
			sIdx++
		case "number":
			if nIdx >= 3 {
				return nil, fmt.Errorf("%w: collection %q exceeds max 3 'number' index slots", ErrBadManifest, name)
			}
			sb.kind = slotNumber
			sb.column = fmt.Sprintf("n%d", nIdx+1)
			nIdx++
		case "time":
			if tIdx >= 2 {
				return nil, fmt.Errorf("%w: collection %q exceeds max 2 'time' index slots", ErrBadManifest, name)
			}
			sb.kind = slotTime
			sb.column = fmt.Sprintf("t%d", tIdx+1)
			tIdx++
		default:
			return nil, fmt.Errorf("%w: unsupported index type %q for %s.%s (want string, string:unique, number, time)",
				ErrBadManifest, typ, name, f)
		}
		cc.byField[f] = sb
		cc.slots = append(cc.slots, sb)
	}
	return cc, nil
}

// DocStore manages all declared collections for a single plugin on plugin_docs.
type DocStore struct {
	pluginID    string
	db          *ent.Client
	maxDocs     int
	perms       *PermissionSet
	collections map[string]*compiledCollection
}

// NewDocStore compiles declared collection index mappings for pluginID.
func NewDocStore(pluginID string, client *ent.Client, spec StorageManifest, perms *PermissionSet) (*DocStore, error) {
	maxDocs := spec.Quota.Docs
	if maxDocs <= 0 {
		maxDocs = MaxPluginDocs
	}
	colls := make(map[string]*compiledCollection, len(spec.Collections))
	for name, c := range spec.Collections {
		cc, err := compileCollectionSlots(name, c)
		if err != nil {
			return nil, err
		}
		colls[name] = cc
	}
	return &DocStore{
		pluginID:    pluginID,
		db:          client,
		maxDocs:     maxDocs,
		perms:       perms,
		collections: colls,
	}, nil
}

// Collection returns a bound collection handle if declared in manifest.
func (ds *DocStore) Collection(name string) (*DocCollection, error) {
	if err := ds.perms.Check(PermStorageDocs); err != nil {
		return nil, err
	}
	cc, ok := ds.collections[name]
	if !ok {
		return nil, fmt.Errorf("%w: %q", ErrUnknownCollection, name)
	}
	return &DocCollection{store: ds, cc: cc}, nil
}

// DocCollection queries and mutates one plugin document collection via slot columns.
type DocCollection struct {
	store *DocStore
	cc    *compiledCollection
}

// FindOpts configures pagination and indexed sorting for Find.
type FindOpts struct {
	Sort   map[string]int `json:"sort,omitempty"` // field -> 1 (asc) | -1 (desc)
	Limit  int            `json:"limit,omitempty"`
	Cursor string         `json:"cursor,omitempty"`
}

// FindResult is a page of documents returned by Find.
type FindResult struct {
	Items      []map[string]any `json:"items"`
	NextCursor string           `json:"nextCursor,omitempty"`
}

func newDocID() string {
	var b [12]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func materializeDoc(row *ent.PluginDoc) map[string]any {
	if row == nil {
		return nil
	}
	out := cloneMap(row.Data)
	out["id"] = row.DocID
	return out
}

// InsertOne inserts a document, extracting indexed fields into u1..t2 and returning
// ErrUniqueConstraint on u1/u2 collisions.
func (c *DocCollection) InsertOne(ctx context.Context, doc map[string]any) (map[string]any, error) {
	if err := c.store.perms.Check(PermStorageDocs); err != nil {
		return nil, err
	}
	data := cloneMap(doc)
	docID, _ := data["id"].(string)
	if docID == "" {
		docID = newDocID()
	} else if len(docID) > 64 {
		return nil, fmt.Errorf("doc id exceeds 64 chars")
	}
	data["id"] = docID

	slotVals, err := c.extractSlots(data)
	if err != nil {
		return nil, err
	}

	var saved *ent.PluginDoc
	err = db.WithTx(ctx, c.store.db, func(tx *ent.Client) error {
		cnt, err := tx.PluginDoc.Query().
			Where(plugindoc.Plugin(c.store.pluginID)).
			Count(ctx)
		if err != nil {
			return err
		}
		if cnt >= c.store.maxDocs {
			return fmt.Errorf("%w: max documents (%d) reached", ErrQuotaExceeded, c.store.maxDocs)
		}
		cr := tx.PluginDoc.Create().
			SetPlugin(c.store.pluginID).
			SetCollection(c.cc.name).
			SetDocID(docID).
			SetData(data)
		setSlotsCreate(cr, slotVals)
		saved, err = cr.Save(ctx)
		if err != nil {
			if db.IsConstraint(err) {
				return ErrUniqueConstraint
			}
			return err
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return materializeDoc(saved), nil
}

// FindOne returns the first matching document or nil.
func (c *DocCollection) FindOne(ctx context.Context, filter map[string]any) (map[string]any, error) {
	res, err := c.Find(ctx, filter, FindOpts{Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(res.Items) == 0 {
		return nil, nil
	}
	return res.Items[0], nil
}

// Find executes an indexed query with cursor pagination and indexed sorting.
func (c *DocCollection) Find(ctx context.Context, filter map[string]any, opts FindOpts) (*FindResult, error) {
	if err := c.store.perms.Check(PermStorageDocs); err != nil {
		return nil, err
	}
	preds, err := c.compileFilter(filter)
	if err != nil {
		return nil, err
	}
	orders, err := c.compileSort(opts.Sort)
	if err != nil {
		return nil, err
	}

	limit := opts.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset := 0
	if opts.Cursor != "" {
		switch {
		case strings.HasPrefix(opts.Cursor, "off:"):
			off, err := strconv.Atoi(strings.TrimPrefix(opts.Cursor, "off:"))
			if err != nil || off < 0 {
				return nil, fmt.Errorf("invalid cursor")
			}
			offset = off
		default:
			idStr := strings.TrimPrefix(opts.Cursor, "id:")
			afterID, err := strconv.ParseInt(idStr, 10, 64)
			if err != nil || afterID < 0 {
				return nil, fmt.Errorf("invalid cursor")
			}
			preds = append(preds, plugindoc.IDGT(afterID))
		}
	}

	q := c.store.db.PluginDoc.Query().Where(preds...)
	if len(orders) > 0 {
		orders = append(orders, ent.Asc(plugindoc.FieldID))
		q = q.Order(orders...)
		if offset > 0 {
			q = q.Offset(offset)
		}
	} else {
		q = q.Order(ent.Asc(plugindoc.FieldID))
	}

	rows, err := q.Limit(limit + 1).All(ctx)
	if err != nil {
		return nil, err
	}
	var next string
	if len(rows) > limit {
		if len(orders) > 0 {
			next = "off:" + strconv.Itoa(offset+limit)
		} else {
			next = "id:" + strconv.FormatInt(rows[limit-1].ID, 10)
		}
		rows = rows[:limit]
	}
	items := make([]map[string]any, 0, len(rows))
	for _, r := range rows {
		items = append(items, materializeDoc(r))
	}
	return &FindResult{Items: items, NextCursor: next}, nil
}

// Count counts documents matching filter (all filter fields must be indexed).
func (c *DocCollection) Count(ctx context.Context, filter map[string]any) (int64, error) {
	if err := c.store.perms.Check(PermStorageDocs); err != nil {
		return 0, err
	}
	preds, err := c.compileFilter(filter)
	if err != nil {
		return 0, err
	}
	n, err := c.store.db.PluginDoc.Query().Where(preds...).Count(ctx)
	return int64(n), err
}

// UpdateOne updates the first matching document and returns whether one matched.
func (c *DocCollection) UpdateOne(ctx context.Context, filter, patch map[string]any) (bool, error) {
	n, err := c.update(ctx, filter, patch, 1)
	return n > 0, err
}

// UpdateMany updates all matching documents (up to 1000 per call) and returns count.
func (c *DocCollection) UpdateMany(ctx context.Context, filter, patch map[string]any) (int, error) {
	return c.update(ctx, filter, patch, 1000)
}

func (c *DocCollection) update(ctx context.Context, filter, patch map[string]any, limit int) (int, error) {
	if err := c.store.perms.Check(PermStorageDocs); err != nil {
		return 0, err
	}
	preds, err := c.compileFilter(filter)
	if err != nil {
		return 0, err
	}
	var updated int
	err = db.WithTx(ctx, c.store.db, func(tx *ent.Client) error {
		rows, err := tx.PluginDoc.Query().Where(preds...).Order(ent.Asc(plugindoc.FieldID)).Limit(limit).All(ctx)
		if err != nil {
			return err
		}
		for _, row := range rows {
			merged := applyDocPatch(cloneMap(row.Data), patch)
			merged["id"] = row.DocID
			slotVals, err := c.extractSlots(merged)
			if err != nil {
				return err
			}
			upd := tx.PluginDoc.UpdateOneID(row.ID).SetData(merged)
			setSlotsUpdate(upd, slotVals)
			if err := upd.Exec(ctx); err != nil {
				if db.IsConstraint(err) {
					return ErrUniqueConstraint
				}
				return err
			}
			updated++
		}
		return nil
	})
	return updated, err
}

// DeleteOne deletes the first matching document and returns whether one was deleted.
func (c *DocCollection) DeleteOne(ctx context.Context, filter map[string]any) (bool, error) {
	if err := c.store.perms.Check(PermStorageDocs); err != nil {
		return false, err
	}
	preds, err := c.compileFilter(filter)
	if err != nil {
		return false, err
	}
	row, err := c.store.db.PluginDoc.Query().Where(preds...).Order(ent.Asc(plugindoc.FieldID)).First(ctx)
	if ent.IsNotFound(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	err = c.store.db.PluginDoc.DeleteOneID(row.ID).Exec(ctx)
	if ent.IsNotFound(err) {
		return false, nil
	}
	return err == nil, err
}

func applyDocPatch(doc, patch map[string]any) map[string]any {
	if rawSet, ok := patch["$set"].(map[string]any); ok {
		for k, v := range rawSet {
			if v == nil {
				delete(doc, k)
			} else {
				doc[k] = v
			}
		}
	}
	if rawUnset, ok := patch["$unset"].([]any); ok {
		for _, item := range rawUnset {
			if k, ok := item.(string); ok {
				delete(doc, k)
			}
		}
	}
	for k, v := range patch {
		if k == "$set" || k == "$unset" || k == "id" {
			continue
		}
		if v == nil {
			delete(doc, k)
		} else {
			doc[k] = v
		}
	}
	return doc
}

func (c *DocCollection) extractSlots(doc map[string]any) (map[slotBinding]any, error) {
	vals := make(map[slotBinding]any, len(c.cc.slots))
	for _, sb := range c.cc.slots {
		raw, exists := doc[sb.field]
		if !exists || raw == nil {
			vals[sb] = nil
			continue
		}
		typed, err := coerceSlotValue(sb, raw)
		if err != nil {
			return nil, fmt.Errorf("collection %q field %q: %w", c.cc.name, sb.field, err)
		}
		vals[sb] = typed
	}
	return vals, nil
}

func coerceSlotValue(sb slotBinding, v any) (any, error) {
	switch sb.kind {
	case slotString, slotUniqueString:
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("expected string, got %T", v)
		}
		if len(s) > 255 {
			return nil, fmt.Errorf("indexed string exceeds 255 bytes")
		}
		return s, nil
	case slotNumber:
		switch n := v.(type) {
		case int:
			return float64(n), nil
		case int64:
			return float64(n), nil
		case float64:
			if math.IsNaN(n) || math.IsInf(n, 0) {
				return nil, errors.New("non-finite number")
			}
			return n, nil
		default:
			return nil, fmt.Errorf("expected number, got %T", v)
		}
	case slotTime:
		switch t := v.(type) {
		case time.Time:
			return t.UTC(), nil
		case string:
			parsed, err := time.Parse(time.RFC3339Nano, t)
			if err != nil {
				parsed, err = time.Parse(time.RFC3339, t)
			}
			if err != nil {
				return nil, fmt.Errorf("expected ISO-8601 time string: %w", err)
			}
			return parsed.UTC(), nil
		case float64:
			ms := int64(t)
			return time.UnixMilli(ms).UTC(), nil
		case int64:
			return time.UnixMilli(t).UTC(), nil
		default:
			return nil, fmt.Errorf("expected time, got %T", v)
		}
	default:
		return nil, errors.New("unknown slot kind")
	}
}

func (c *DocCollection) lookupSlot(field string) (slotBinding, bool) {
	if sb, ok := c.cc.byField[field]; ok {
		return sb, true
	}
	if field == "id" {
		return slotBinding{field: "id", column: plugindoc.FieldDocID, kind: slotUniqueString}, true
	}
	return slotBinding{}, false
}

func (c *DocCollection) compileFilter(filter map[string]any) ([]predicate.PluginDoc, error) {
	preds := []predicate.PluginDoc{
		plugindoc.Plugin(c.store.pluginID),
		plugindoc.Collection(c.cc.name),
	}
	for k, raw := range filter {
		sb, ok := c.lookupSlot(k)
		if !ok {
			return nil, &UnindexedFieldError{Collection: c.cc.name, Field: k}
		}
		if opMap, isObj := raw.(map[string]any); isObj {
			for op, operand := range opMap {
				p, err := buildSlotOpPredicate(sb, op, operand)
				if err != nil {
					return nil, err
				}
				preds = append(preds, p)
			}
			continue
		}
		if raw == nil {
			preds = append(preds, slotIsNilPred(sb.column))
			continue
		}
		typed, err := coerceSlotValue(sb, raw)
		if err != nil {
			return nil, err
		}
		preds = append(preds, slotEQPred(sb.column, typed))
	}
	return preds, nil
}

func (c *DocCollection) compileSort(sortSpec map[string]int) ([]plugindoc.OrderOption, error) {
	if len(sortSpec) == 0 {
		return nil, nil
	}
	keys := make([]string, 0, len(sortSpec))
	for k := range sortSpec {
		if _, ok := c.lookupSlot(k); !ok {
			return nil, &UnindexedFieldError{Collection: c.cc.name, Field: k}
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	orders := make([]plugindoc.OrderOption, 0, len(keys))
	for _, k := range keys {
		sb, _ := c.lookupSlot(k)
		if sortSpec[k] < 0 {
			orders = append(orders, ent.Desc(sb.column))
		} else {
			orders = append(orders, ent.Asc(sb.column))
		}
	}
	return orders, nil
}

func buildSlotOpPredicate(sb slotBinding, op string, operand any) (predicate.PluginDoc, error) {
	if operand == nil {
		switch op {
		case "$eq":
			return slotIsNilPred(sb.column), nil
		case "$ne":
			return slotNotNilPred(sb.column), nil
		default:
			return nil, fmt.Errorf("null operand unsupported for %s", op)
		}
	}
	if op == "$in" {
		sliceVal, ok := operand.([]any)
		if !ok {
			return nil, fmt.Errorf("$in requires an array")
		}
		coerced := make([]any, 0, len(sliceVal))
		for _, item := range sliceVal {
			v, err := coerceSlotValue(sb, item)
			if err != nil {
				return nil, err
			}
			coerced = append(coerced, v)
		}
		return slotInPred(sb.column, coerced)
	}
	if op == "$prefix" {
		if sb.kind != slotString && sb.kind != slotUniqueString {
			return nil, fmt.Errorf("$prefix requires string slot")
		}
		s, ok := operand.(string)
		if !ok {
			return nil, fmt.Errorf("$prefix requires string value")
		}
		return slotPrefixPred(sb.column, s)
	}
	v, err := coerceSlotValue(sb, operand)
	if err != nil {
		return nil, err
	}
	return slotCmpPred(sb.column, op, v)
}

func setSlotsCreate(cr *ent.PluginDocCreate, vals map[slotBinding]any) {
	for sb, v := range vals {
		if v == nil {
			continue
		}
		switch sb.column {
		case "u1":
			cr.SetU1(v.(string))
		case "u2":
			cr.SetU2(v.(string))
		case "s1":
			cr.SetS1(v.(string))
		case "s2":
			cr.SetS2(v.(string))
		case "s3":
			cr.SetS3(v.(string))
		case "n1":
			cr.SetN1(v.(float64))
		case "n2":
			cr.SetN2(v.(float64))
		case "n3":
			cr.SetN3(v.(float64))
		case "t1":
			cr.SetT1(v.(time.Time))
		case "t2":
			cr.SetT2(v.(time.Time))
		}
	}
}

func setSlotsUpdate(up *ent.PluginDocUpdateOne, vals map[slotBinding]any) {
	for sb, v := range vals {
		switch sb.column {
		case "u1":
			if v == nil {
				up.ClearU1()
			} else {
				up.SetU1(v.(string))
			}
		case "u2":
			if v == nil {
				up.ClearU2()
			} else {
				up.SetU2(v.(string))
			}
		case "s1":
			if v == nil {
				up.ClearS1()
			} else {
				up.SetS1(v.(string))
			}
		case "s2":
			if v == nil {
				up.ClearS2()
			} else {
				up.SetS2(v.(string))
			}
		case "s3":
			if v == nil {
				up.ClearS3()
			} else {
				up.SetS3(v.(string))
			}
		case "n1":
			if v == nil {
				up.ClearN1()
			} else {
				up.SetN1(v.(float64))
			}
		case "n2":
			if v == nil {
				up.ClearN2()
			} else {
				up.SetN2(v.(float64))
			}
		case "n3":
			if v == nil {
				up.ClearN3()
			} else {
				up.SetN3(v.(float64))
			}
		case "t1":
			if v == nil {
				up.ClearT1()
			} else {
				up.SetT1(v.(time.Time))
			}
		case "t2":
			if v == nil {
				up.ClearT2()
			} else {
				up.SetT2(v.(time.Time))
			}
		}
	}
}

func slotIsNilPred(col string) predicate.PluginDoc {
	switch col {
	case plugindoc.FieldDocID:
		return plugindoc.DocID("")
	case "u1":
		return plugindoc.U1IsNil()
	case "u2":
		return plugindoc.U2IsNil()
	case "s1":
		return plugindoc.S1IsNil()
	case "s2":
		return plugindoc.S2IsNil()
	case "s3":
		return plugindoc.S3IsNil()
	case "n1":
		return plugindoc.N1IsNil()
	case "n2":
		return plugindoc.N2IsNil()
	case "n3":
		return plugindoc.N3IsNil()
	case "t1":
		return plugindoc.T1IsNil()
	case "t2":
		return plugindoc.T2IsNil()
	default:
		return nil
	}
}

func slotNotNilPred(col string) predicate.PluginDoc {
	switch col {
	case plugindoc.FieldDocID:
		return plugindoc.DocIDNEQ("")
	case "u1":
		return plugindoc.U1NotNil()
	case "u2":
		return plugindoc.U2NotNil()
	case "s1":
		return plugindoc.S1NotNil()
	case "s2":
		return plugindoc.S2NotNil()
	case "s3":
		return plugindoc.S3NotNil()
	case "n1":
		return plugindoc.N1NotNil()
	case "n2":
		return plugindoc.N2NotNil()
	case "n3":
		return plugindoc.N3NotNil()
	case "t1":
		return plugindoc.T1NotNil()
	case "t2":
		return plugindoc.T2NotNil()
	default:
		return nil
	}
}

func slotEQPred(col string, v any) predicate.PluginDoc {
	switch col {
	case plugindoc.FieldDocID:
		return plugindoc.DocID(v.(string))
	case "u1":
		return plugindoc.U1(v.(string))
	case "u2":
		return plugindoc.U2(v.(string))
	case "s1":
		return plugindoc.S1(v.(string))
	case "s2":
		return plugindoc.S2(v.(string))
	case "s3":
		return plugindoc.S3(v.(string))
	case "n1":
		return plugindoc.N1(v.(float64))
	case "n2":
		return plugindoc.N2(v.(float64))
	case "n3":
		return plugindoc.N3(v.(float64))
	case "t1":
		return plugindoc.T1(v.(time.Time))
	case "t2":
		return plugindoc.T2(v.(time.Time))
	default:
		return nil
	}
}

func slotPrefixPred(col, s string) (predicate.PluginDoc, error) {
	switch col {
	case plugindoc.FieldDocID:
		return plugindoc.DocIDHasPrefix(s), nil
	case "u1":
		return plugindoc.U1HasPrefix(s), nil
	case "u2":
		return plugindoc.U2HasPrefix(s), nil
	case "s1":
		return plugindoc.S1HasPrefix(s), nil
	case "s2":
		return plugindoc.S2HasPrefix(s), nil
	case "s3":
		return plugindoc.S3HasPrefix(s), nil
	default:
		return nil, fmt.Errorf("$prefix unsupported for %s", col)
	}
}

func slotInPred(col string, vals []any) (predicate.PluginDoc, error) {
	switch {
	case col == plugindoc.FieldDocID, strings.HasPrefix(col, "u"), strings.HasPrefix(col, "s"):
		ss := make([]string, len(vals))
		for i, v := range vals {
			ss[i] = v.(string)
		}
		switch col {
		case plugindoc.FieldDocID:
			return plugindoc.DocIDIn(ss...), nil
		case "u1":
			return plugindoc.U1In(ss...), nil
		case "u2":
			return plugindoc.U2In(ss...), nil
		case "s1":
			return plugindoc.S1In(ss...), nil
		case "s2":
			return plugindoc.S2In(ss...), nil
		case "s3":
			return plugindoc.S3In(ss...), nil
		}
	case strings.HasPrefix(col, "n"):
		ns := make([]float64, len(vals))
		for i, v := range vals {
			ns[i] = v.(float64)
		}
		switch col {
		case "n1":
			return plugindoc.N1In(ns...), nil
		case "n2":
			return plugindoc.N2In(ns...), nil
		case "n3":
			return plugindoc.N3In(ns...), nil
		}
	case strings.HasPrefix(col, "t"):
		ts := make([]time.Time, len(vals))
		for i, v := range vals {
			ts[i] = v.(time.Time)
		}
		switch col {
		case "t1":
			return plugindoc.T1In(ts...), nil
		case "t2":
			return plugindoc.T2In(ts...), nil
		}
	}
	return nil, fmt.Errorf("unsupported slot %s for $in", col)
}

func slotCmpPred(col, op string, v any) (predicate.PluginDoc, error) {
	switch col {
	case plugindoc.FieldDocID:
		s := v.(string)
		switch op {
		case "$eq":
			return plugindoc.DocIDEQ(s), nil
		case "$ne":
			return plugindoc.DocIDNEQ(s), nil
		case "$gt":
			return plugindoc.DocIDGT(s), nil
		case "$gte":
			return plugindoc.DocIDGTE(s), nil
		case "$lt":
			return plugindoc.DocIDLT(s), nil
		case "$lte":
			return plugindoc.DocIDLTE(s), nil
		}
	case "u1":
		s := v.(string)
		switch op {
		case "$eq":
			return plugindoc.U1EQ(s), nil
		case "$ne":
			return plugindoc.U1NEQ(s), nil
		case "$gt":
			return plugindoc.U1GT(s), nil
		case "$gte":
			return plugindoc.U1GTE(s), nil
		case "$lt":
			return plugindoc.U1LT(s), nil
		case "$lte":
			return plugindoc.U1LTE(s), nil
		}
	case "u2":
		s := v.(string)
		switch op {
		case "$eq":
			return plugindoc.U2EQ(s), nil
		case "$ne":
			return plugindoc.U2NEQ(s), nil
		case "$gt":
			return plugindoc.U2GT(s), nil
		case "$gte":
			return plugindoc.U2GTE(s), nil
		case "$lt":
			return plugindoc.U2LT(s), nil
		case "$lte":
			return plugindoc.U2LTE(s), nil
		}
	case "s1":
		s := v.(string)
		switch op {
		case "$eq":
			return plugindoc.S1EQ(s), nil
		case "$ne":
			return plugindoc.S1NEQ(s), nil
		case "$gt":
			return plugindoc.S1GT(s), nil
		case "$gte":
			return plugindoc.S1GTE(s), nil
		case "$lt":
			return plugindoc.S1LT(s), nil
		case "$lte":
			return plugindoc.S1LTE(s), nil
		}
	case "s2":
		s := v.(string)
		switch op {
		case "$eq":
			return plugindoc.S2EQ(s), nil
		case "$ne":
			return plugindoc.S2NEQ(s), nil
		case "$gt":
			return plugindoc.S2GT(s), nil
		case "$gte":
			return plugindoc.S2GTE(s), nil
		case "$lt":
			return plugindoc.S2LT(s), nil
		case "$lte":
			return plugindoc.S2LTE(s), nil
		}
	case "s3":
		s := v.(string)
		switch op {
		case "$eq":
			return plugindoc.S3EQ(s), nil
		case "$ne":
			return plugindoc.S3NEQ(s), nil
		case "$gt":
			return plugindoc.S3GT(s), nil
		case "$gte":
			return plugindoc.S3GTE(s), nil
		case "$lt":
			return plugindoc.S3LT(s), nil
		case "$lte":
			return plugindoc.S3LTE(s), nil
		}
	case "n1":
		n := v.(float64)
		switch op {
		case "$eq":
			return plugindoc.N1EQ(n), nil
		case "$ne":
			return plugindoc.N1NEQ(n), nil
		case "$gt":
			return plugindoc.N1GT(n), nil
		case "$gte":
			return plugindoc.N1GTE(n), nil
		case "$lt":
			return plugindoc.N1LT(n), nil
		case "$lte":
			return plugindoc.N1LTE(n), nil
		}
	case "n2":
		n := v.(float64)
		switch op {
		case "$eq":
			return plugindoc.N2EQ(n), nil
		case "$ne":
			return plugindoc.N2NEQ(n), nil
		case "$gt":
			return plugindoc.N2GT(n), nil
		case "$gte":
			return plugindoc.N2GTE(n), nil
		case "$lt":
			return plugindoc.N2LT(n), nil
		case "$lte":
			return plugindoc.N2LTE(n), nil
		}
	case "n3":
		n := v.(float64)
		switch op {
		case "$eq":
			return plugindoc.N3EQ(n), nil
		case "$ne":
			return plugindoc.N3NEQ(n), nil
		case "$gt":
			return plugindoc.N3GT(n), nil
		case "$gte":
			return plugindoc.N3GTE(n), nil
		case "$lt":
			return plugindoc.N3LT(n), nil
		case "$lte":
			return plugindoc.N3LTE(n), nil
		}
	case "t1":
		t := v.(time.Time)
		switch op {
		case "$eq":
			return plugindoc.T1EQ(t), nil
		case "$ne":
			return plugindoc.T1NEQ(t), nil
		case "$gt":
			return plugindoc.T1GT(t), nil
		case "$gte":
			return plugindoc.T1GTE(t), nil
		case "$lt":
			return plugindoc.T1LT(t), nil
		case "$lte":
			return plugindoc.T1LTE(t), nil
		}
	case "t2":
		t := v.(time.Time)
		switch op {
		case "$eq":
			return plugindoc.T2EQ(t), nil
		case "$ne":
			return plugindoc.T2NEQ(t), nil
		case "$gt":
			return plugindoc.T2GT(t), nil
		case "$gte":
			return plugindoc.T2GTE(t), nil
		case "$lt":
			return plugindoc.T2LT(t), nil
		case "$lte":
			return plugindoc.T2LTE(t), nil
		}
	}
	return nil, fmt.Errorf("unsupported query operator %q for %s", op, col)
}
