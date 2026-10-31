package serve

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const wiringStubs = `
var __calls = [], __mode = 'ok', __els = {};
class FE {
  constructor(tag){ this.tag = tag; this.children = []; this.hidden = false; this.checked = false;
    this.textContent = ''; this.className = ''; this.handlers = {}; this.disabled = false; this.scrollTop = 0; this.scrollHeight = 0; this.value = ''; }
  appendChild(c){ this.children.push(c); return c; }
  replaceChildren(...a){ this.children = a; }
  addEventListener(t, f){ this.handlers[t] = f; }
  requestSubmit(){}
  all(){ return this.children.flatMap(c => [c, ...c.all()]); }
  querySelectorAll(sel){ return this.all().filter(e => e.tag === sel); }
}
var document = { getElementById: id => (__els[id] = __els[id] || new FE('div')), createElement: t => new FE(t) };
var window = { addEventListener(t, f){ this.handlers = this.handlers || {}; this.handlers[t] = f; } };
function fetch(url, opts){
  if(String(url).endsWith('/approve')){
    __calls.push({ url: url, body: JSON.parse(opts.body) });
    if(__mode === 'reject') return Promise.reject(new Error('net down'));
    return Promise.resolve({ ok: __mode === 'ok', status: __mode === 'ok' ? 200 : 500 });
  }
  return Promise.resolve({ ok: true, status: 200, json: async () => [] });
}
`

const wiringCases = `
;(async () => {
  const assert = require('assert');
  const settle = () => new Promise(r => setTimeout(r, 5));
  const log = () => document.getElementById('log');
  const buttons = () => log().querySelectorAll('button');
  const ev = (tool, args) => JSON.stringify({ approval_id: 'ap1', tool: tool, args: args === undefined ? '{"cmd":"ls"}' : args });
  const fire = async (data) => { handle({ el: null }, 'approval-needed', data); await settle(); };
  const reset = (on, mode) => { log().replaceChildren(); __calls.length = 0; __mode = mode || 'ok'; sessionId = 'S1'; setAuto(on); };
  const isOn = () => autoOn === true && document.getElementById('autoApprove').checked && !document.getElementById('autoBar').hidden;
  const isOff = () => autoOn === false && !document.getElementById('autoApprove').checked && document.getElementById('autoBar').hidden;

  // init: starts off
  assert(isOff(), 'initial state off');

  // (a)
  reset(true); await fire(ev('run_shell'));
  assert.strictEqual(__calls.length, 1);
  assert.strictEqual(__calls[0].url, '/session/S1/approve');
  assert.deepStrictEqual(__calls[0].body, { approval_id: 'ap1', approved: true });
  assert.strictEqual(buttons().length, 0, 'no card buttons');
  assert.strictEqual(log().children[0].children[0].textContent, 'Auto approved: run_shell');

  // (b)
  reset(true); await fire(ev('brief_create'));
  assert.strictEqual(__calls.length, 0); assert.strictEqual(buttons().length, 2);

  // (c)
  reset(true); await fire(ev('run_shell', JSON.stringify({ a: 'x'.repeat(200001) })));
  assert.strictEqual(__calls.length, 0); assert.strictEqual(buttons().length, 2);
  assert(buttons()[0].disabled, 'Approve disabled when truncated');

  // (d) failed POST
  reset(true, 'fail'); await fire(ev('run_shell'));
  assert.strictEqual(__calls.length, 1); assert.strictEqual(buttons().length, 2);
  assert(log().children[0].children[0].textContent.startsWith('Auto approve failed'));
  // (d) network error
  reset(true, 'reject'); await fire(ev('run_shell'));
  assert.strictEqual(buttons().length, 2);
  assert(log().all().some(e => String(e.textContent).includes('may already have been applied')), 'network note');

  // (e) openSession and New chat reset
  reset(true); assert(isOn());
  await openSession('S2'); assert(isOff(), 'openSession resets');
  setAuto(true); assert(isOn());
  document.getElementById('newBtn').handlers.click(); assert(isOff(), 'new chat resets');
  setAuto(true); window.handlers.pageshow({ persisted: true }); assert(isOff(), 'pageshow resets');
  setAuto(true); window.handlers.pageshow({ persisted: false }); assert(isOn(), 'non-persisted pageshow keeps');
  document.getElementById('autoOff').handlers.click(); assert(isOff());

  // (f) toggled off via checkbox, then event
  reset(false);
  const cb = document.getElementById('autoApprove');
  cb.checked = true; cb.handlers.change({ target: cb }); assert(isOn());
  cb.checked = false; cb.handlers.change({ target: cb }); assert(isOff());
  await fire(ev('run_shell'));
  assert.strictEqual(__calls.length, 0); assert.strictEqual(buttons().length, 2);
  console.log('ok');
})().catch(e => { console.error(e); process.exit(1); });
`

func TestChatAutoApproveWiring(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not on PATH; skipping chat page wiring test")
	}
	body := chatBody(t)
	m := strings.Index(body, autoBegin)
	start := strings.LastIndex(body[:m], "<script>")
	end := strings.Index(body[m:], "</script>")
	if m < 0 || start < 0 || end < 0 {
		t.Fatal("cannot locate the chat page script")
	}
	script := body[start+len("<script>") : m+end]
	dir := t.TempDir()
	js := "const vm = require('vm');\nconst ctx = vm.createContext({ console, setTimeout, process, require });\n" +
		"vm.runInContext(" + jsString(wiringStubs) + ", ctx);\n" +
		"vm.runInContext(" + jsString(script+wiringCases) + ", ctx);\n"
	f := filepath.Join(dir, "wiring.js")
	if err := os.WriteFile(f, []byte(js), 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, f).CombinedOutput()
	if err != nil {
		t.Fatalf("node: %v\n%s", err, out)
	}
}

func jsString(s string) string {
	r := strings.NewReplacer("\\", "\\\\", "`", "\\`", "${", "\\${")
	return "`" + r.Replace(s) + "`"
}
