package contract

import "testing"

const receiverContract = `{"spec_version": "2.0", "brief_id": "gm-2026-09-29-900", "revision": 0, "module": "example.com/x",
 "conventions": {"layout": ["internal/x"], "naming": ["verbs"], "errors": "return error"},
 "types": [
  {"id": "col-state", "package": "x", "file": "internal/x/x.go", "decl": "type colState struct {\n\tname string\n}"},
  {"id": "other-state", "package": "y", "file": "internal/y/y.go", "decl": "type colState struct {\n\tother int\n}"},
  {"id": "box", "package": "x", "file": "internal/x/x.go", "decl": "type Box[T any] struct {\n\tv T\n}"}],
 "functions": [
  {"id": "fn-ptr", "package": "x", "file": "internal/x/a.go", "signature": "func (s *colState) finalize(name string) string", "doc": "d", "uses": [], "component": "c"},
  {"id": "fn-val", "package": "x", "file": "internal/x/b.go", "signature": "func (s colState) Name() string", "doc": "d", "uses": [], "component": "c"},
  {"id": "fn-gen", "package": "x", "file": "internal/x/c.go", "signature": "func (b *Box[T]) Get() T", "doc": "d", "uses": [], "component": "c"},
  {"id": "fn-ypkg", "package": "y", "file": "internal/y/z.go", "signature": "func (s *colState) Other() int", "doc": "d", "uses": [], "component": "c"},
  {"id": "fn-plain", "package": "x", "file": "internal/x/d.go", "signature": "func Plain() string", "doc": "d", "uses": [], "component": "c"},
  {"id": "fn-none", "package": "x", "file": "internal/x/e.go", "signature": "func (s *ghost) Run()", "doc": "d", "uses": [], "component": "c"}],
 "components": [{"id": "c", "package": "x", "exports": []}]}`

func TestReceiverTypeID(t *testing.T) {
	c, err := Load([]byte(receiverContract))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"fn-ptr": "col-state", "fn-val": "col-state", "fn-gen": "box",
		"fn-ypkg": "other-state", "fn-plain": "", "fn-none": "",
	}
	for _, f := range c.Functions {
		if got := c.ReceiverTypeID(f); got != want[f.ID] {
			t.Errorf("%s: ReceiverTypeID = %q, want %q", f.ID, got, want[f.ID])
		}
	}
}
