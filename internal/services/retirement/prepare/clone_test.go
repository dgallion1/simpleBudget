package prepare

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"budget2/internal/models"
)

// TestCloneCarriesEveryJSONOmittedField is the anti-rot guard on Clone.
//
// prepare.DeepCopy round-trips through JSON, so every field tagged json:"-"
// is silently dropped. Clone exists to carry them across. This test does NOT
// hard-code the list of such fields: it reflects over the
// models.WhatIfSettings type graph, finds every json:"-" field, sets a
// distinctive non-zero value at that field, clones, and asserts the value
// survived. Adding a new json:"-" field anywhere reachable from
// WhatIfSettings without teaching carryJSONOmittedFields about it fails here.
//
// If the walk reaches a json:"-" field behind a hop this test cannot navigate
// (a map value, an interface), it fails loudly asking for the test to be
// extended, rather than skipping the field.
func TestCloneCarriesEveryJSONOmittedField(t *testing.T) {
	paths := findJSONOmittedFields(t, reflect.TypeOf(models.WhatIfSettings{}))

	// Lower-bound sanity check on the WALK itself, not on Clone: if the
	// reflection ever silently stops descending, the per-field assertions
	// below would all vacuously pass. These three are the json:"-" fields
	// reachable from WhatIfSettings as of this test's writing; the check is
	// deliberately "contains", so new fields are welcome and still get their
	// own assertion below.
	for _, want := range []string{
		"CurrentAge",
		"SpouseAge",
		"RothConversion.PerYearOverrides",
	} {
		if !containsPath(paths, want) {
			t.Fatalf("reflection walk did not find known json:%q field %s; found %v",
				"-", want, joinPaths(paths))
		}
	}

	for _, path := range paths {
		t.Run(strings.Join(path, "."), func(t *testing.T) {
			src := &models.WhatIfSettings{}

			leaf, err := navigate(reflect.ValueOf(src).Elem(), path, true)
			if err != nil {
				t.Fatalf("cannot reach %s: %v\n"+
					"a json:\"-\" field now sits behind a hop this test cannot navigate; "+
					"extend navigate() and carryJSONOmittedFields()", strings.Join(path, "."), err)
			}
			if err := setDistinctive(leaf); err != nil {
				t.Fatalf("cannot populate %s: %v\n"+
					"extend setDistinctive() and carryJSONOmittedFields()", strings.Join(path, "."), err)
			}
			want := deepCopyValue(leaf)

			dst, err := Clone(src)
			if err != nil {
				t.Fatalf("Clone: %v", err)
			}

			got, err := navigate(reflect.ValueOf(dst).Elem(), path, false)
			if err != nil {
				t.Fatalf("Clone dropped the path to %s: %v", strings.Join(path, "."), err)
			}
			if !reflect.DeepEqual(got.Interface(), want.Interface()) {
				t.Fatalf("Clone did not carry json:\"-\" field %s: got %#v, want %#v\n"+
					"add it to carryJSONOmittedFields", strings.Join(path, "."), got.Interface(), want.Interface())
			}

			// A carried reference type must be copied, not aliased: Clone's
			// contract is that mutating one settings object is never
			// observable through another.
			switch got.Kind() {
			case reflect.Map:
				if got.Pointer() == leaf.Pointer() {
					t.Fatalf("Clone aliased the map at %s instead of copying it", strings.Join(path, "."))
				}
			case reflect.Slice:
				if got.Len() > 0 && got.Index(0).Addr().Pointer() == leaf.Index(0).Addr().Pointer() {
					t.Fatalf("Clone aliased the slice backing array at %s instead of copying it",
						strings.Join(path, "."))
				}
			}
		})
	}
}

// TestCloneMatchesDeepCopyForJSONVisibleFields pins the other half of Clone's
// contract: it is DeepCopy plus the omitted fields, never DeepCopy minus
// something.
func TestCloneMatchesDeepCopyForJSONVisibleFields(t *testing.T) {
	src := models.DefaultWhatIfSettings()
	src.CurrentAge = 61
	src.SpouseAge = 59

	clone, err := Clone(src)
	if err != nil {
		t.Fatalf("Clone: %v", err)
	}

	srcJSON, err := json.Marshal(src)
	if err != nil {
		t.Fatalf("marshal src: %v", err)
	}
	cloneJSON, err := json.Marshal(clone)
	if err != nil {
		t.Fatalf("marshal clone: %v", err)
	}
	if string(srcJSON) != string(cloneJSON) {
		t.Fatalf("Clone changed the JSON-visible state:\n src   = %s\n clone = %s", srcJSON, cloneJSON)
	}
	if clone == src {
		t.Fatal("Clone returned the same pointer")
	}
}

// TestCloneRejectsNil documents the nil contract.
func TestCloneRejectsNil(t *testing.T) {
	if _, err := Clone(nil); err == nil {
		t.Fatal("Clone(nil) = nil error, want an error")
	}
}

// findJSONOmittedFields walks t and returns the dotted field path of every
// field tagged exactly `json:"-"` (which is what makes encoding/json skip it;
// `json:"-,"` names a field "-" and is NOT skipped).
func findJSONOmittedFields(t *testing.T, root reflect.Type) [][]string {
	t.Helper()

	var found [][]string
	inProgress := map[reflect.Type]bool{}

	var walk func(typ reflect.Type, prefix []string)
	walk = func(typ reflect.Type, prefix []string) {
		typ = elemType(typ)
		if typ.Kind() != reflect.Struct {
			return
		}
		if inProgress[typ] {
			return // recursive type; the fields were already enumerated once
		}
		inProgress[typ] = true
		defer delete(inProgress, typ)

		for i := 0; i < typ.NumField(); i++ {
			field := typ.Field(i)
			if !field.IsExported() {
				continue
			}
			path := append(append([]string{}, prefix...), field.Name)
			if field.Tag.Get("json") == "-" {
				found = append(found, path)
				continue
			}
			walk(field.Type, path)
		}
	}
	walk(root, nil)

	return found
}

// elemType peels pointer/slice/array/map wrappers off t so the walk can see
// the struct underneath.
func elemType(t reflect.Type) reflect.Type {
	for {
		switch t.Kind() {
		case reflect.Pointer, reflect.Slice, reflect.Array, reflect.Map:
			t = t.Elem()
		default:
			return t
		}
	}
}

// navigate walks v down path and returns the addressable leaf value. With
// allocate set it materializes nil pointers and empty slices along the way;
// without it, a missing hop is an error (which is itself a Clone failure —
// the copy lost the container the field lived in).
func navigate(v reflect.Value, path []string, allocate bool) (reflect.Value, error) {
	for i, name := range path {
		for {
			switch v.Kind() {
			case reflect.Pointer:
				if v.IsNil() {
					if !allocate {
						return reflect.Value{}, errAt(path, i, "nil pointer")
					}
					v.Set(reflect.New(v.Type().Elem()))
				}
				v = v.Elem()
				continue
			case reflect.Slice:
				if v.Len() == 0 {
					if !allocate {
						return reflect.Value{}, errAt(path, i, "empty slice")
					}
					v.Set(reflect.MakeSlice(v.Type(), 1, 1))
				}
				v = v.Index(0)
				continue
			case reflect.Struct:
			default:
				return reflect.Value{}, errAt(path, i, "unsupported hop kind "+v.Kind().String())
			}
			break
		}
		field := v.FieldByName(name)
		if !field.IsValid() {
			return reflect.Value{}, errAt(path, i, "no such field")
		}
		v = field
	}
	return v, nil
}

func errAt(path []string, i int, msg string) error {
	return &navError{at: strings.Join(path[:i+1], "."), msg: msg}
}

type navError struct {
	at  string
	msg string
}

func (e *navError) Error() string { return e.at + ": " + e.msg }

// setDistinctive fills v with a non-zero value that a dropped-then-defaulted
// field could not accidentally equal.
func setDistinctive(v reflect.Value) error {
	switch v.Kind() {
	case reflect.Bool:
		v.SetBool(true)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		v.SetInt(4242)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		v.SetUint(4242)
	case reflect.Float32, reflect.Float64:
		v.SetFloat(4242.5)
	case reflect.String:
		v.SetString("prepare-clone-sentinel")
	case reflect.Pointer:
		v.Set(reflect.New(v.Type().Elem()))
		return setDistinctive(v.Elem())
	case reflect.Slice:
		v.Set(reflect.MakeSlice(v.Type(), 1, 1))
		return setDistinctive(v.Index(0))
	case reflect.Map:
		m := reflect.MakeMap(v.Type())
		key := reflect.New(v.Type().Key()).Elem()
		if err := setDistinctive(key); err != nil {
			return err
		}
		val := reflect.New(v.Type().Elem()).Elem()
		if err := setDistinctive(val); err != nil {
			return err
		}
		m.SetMapIndex(key, val)
		v.Set(m)
	case reflect.Struct:
		for i := 0; i < v.NumField(); i++ {
			if !v.Type().Field(i).IsExported() {
				continue
			}
			if err := setDistinctive(v.Field(i)); err != nil {
				return err
			}
		}
	default:
		return &navError{at: v.Type().String(), msg: "no distinctive value for kind " + v.Kind().String()}
	}
	return nil
}

// deepCopyValue snapshots v so the expectation cannot be mutated by anything
// Clone does to the source.
func deepCopyValue(v reflect.Value) reflect.Value {
	out := reflect.New(v.Type()).Elem()
	switch v.Kind() {
	case reflect.Map:
		if v.IsNil() {
			return out
		}
		m := reflect.MakeMapWithSize(v.Type(), v.Len())
		iter := v.MapRange()
		for iter.Next() {
			m.SetMapIndex(iter.Key(), deepCopyValue(iter.Value()))
		}
		out.Set(m)
	case reflect.Slice:
		if v.IsNil() {
			return out
		}
		s := reflect.MakeSlice(v.Type(), v.Len(), v.Len())
		for i := 0; i < v.Len(); i++ {
			s.Index(i).Set(deepCopyValue(v.Index(i)))
		}
		out.Set(s)
	case reflect.Pointer:
		if v.IsNil() {
			return out
		}
		p := reflect.New(v.Type().Elem())
		p.Elem().Set(deepCopyValue(v.Elem()))
		out.Set(p)
	default:
		out.Set(v)
	}
	return out
}

func containsPath(paths [][]string, dotted string) bool {
	for _, p := range paths {
		if strings.Join(p, ".") == dotted {
			return true
		}
	}
	return false
}

func joinPaths(paths [][]string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, strings.Join(p, "."))
	}
	return out
}
