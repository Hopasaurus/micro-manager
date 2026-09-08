/*
  mm.test.js — the behavioural suite for mm.js (T-0111, Option B).

  mm.js is the largest untested surface in the service: everything else has
  tests, the client script has source-shape greps. This suite drives the real
  script against a real DOM (jsdom) and a double for htmx, and asserts the
  effects §7.3 and the data-* attributes specify — never implementation
  internals.

  What it can and cannot catch (details/T-0111.md): wiring bugs — a handler
  that does nothing, a missing attribute, an operation posting an empty body
  — need no layout and this catches them. Layout-coupled bugs — comparing
  coordinates while mutating the DOM being measured (T-0110) — jsdom cannot
  see at all; those are prevented structurally. The drag tests here drive
  events explicitly with synthetic geometry, the way details/T-0141 prescribes.

  The suite runs under `node --test` from a Go test (client_test.go), which
  skips it when node or the jsdom dependency is absent, exactly as the
  check.sh cross-check skips when bash is missing.
*/

'use strict';

const test = require('node:test');
const assert = require('node:assert');
const fs = require('node:fs');
const path = require('node:path');

const { JSDOM } = require('jsdom');

const MM_JS = fs.readFileSync(path.join(__dirname, '..', 'static', 'mm.js'), 'utf8');
const MARKDOWN_IT = require(path.join(__dirname, '..', 'static', 'markdown-it.min.js'));
const CODEMIRROR_JS = fs.readFileSync(path.join(__dirname, '..', 'static', 'codemirror.min.js'), 'utf8');

// The board fixture. Minimal, but faithful to the real render: the app root
// carries data-project-id and the sse/morph extensions; the board, status and
// (absent here) check regions self-describe with hx-get + sse: trigger +
// morph; a card menu with one dialog op, one posting op and one panel op.
const BOARD_HTML = `
<div data-testid="app" data-project-id="x" hx-ext="sse,morph">
  <section data-testid="board" class="mm-board"
           hx-get="/p/x/board?fragment=1" hx-ext="morph"
           hx-trigger="sse:board from:body" hx-swap="morph">
    <section data-testid="board-column-ready" class="mm-column">
      <div data-testid="board-column-ready-body" class="mm-column__body" data-column="ready">
        <article data-testid="item-T-0001" class="mm-item" data-item-id="T-0001" tabindex="0">
          <h3><a href="/p/x/item/T-0001">First</a></h3>
          <button data-testid="item-T-0001-menu" class="mm-item__menu" type="button"
                  aria-expanded="false" aria-label="Actions for T-0001">…</button>
          <div data-testid="item-T-0001-menu-items" class="mm-item__actions" role="menu" hidden>
            <button data-testid="item-T-0001-action-start" class="mm-item__action" role="menuitem"
                    type="button" data-op="start" data-item="T-0001">Start</button>
            <button data-testid="item-T-0001-action-remove" class="mm-item__action" role="menuitem"
                    type="button" data-op="remove" data-item="T-0001">Remove</button>
            <button data-testid="item-T-0001-action-edit" class="mm-item__action" role="menuitem"
                    type="button" data-op="edit" data-item="T-0001">Edit</button>
            <button data-testid="item-T-0001-action-fail" class="mm-item__action" role="menuitem"
                    type="button" disabled data-reason="Conflict" data-op="start" data-item="T-0001">Start</button>
          </div>
        </article>
        <article data-testid="item-T-0002" class="mm-item" data-item-id="T-0002" tabindex="0">
          <h3><a href="/p/x/item/T-0002">Second</a></h3>
        </article>
      </div>
    </section>
    <section data-testid="board-column-done" class="mm-column">
      <div data-testid="board-column-done-body" class="mm-column__body" data-column="done"></div>
    </section>
  </section>
  <footer data-testid="app-status" class="mm-status"
          hx-get="/p/x/status" hx-ext="morph"
          hx-trigger="sse:status from:body" hx-swap="morph">
    <span data-testid="status-wip" class="mm-status__wip">WIP 1/4</span>
  </footer>
  <div data-testid="toast-region" class="mm-toasts"></div>
  <div data-testid="dialog-root" class="mm-dialogs"></div>
</div>
`;

// The same board with a Someday column in front of ready, collapsed the way
// §5.5 renders it: data-collapsed="true", and a body that mm.css hides. jsdom
// applies no stylesheet, so the hiding is not what this fixture reproduces —
// what it reproduces is the consequence, a pointer event that lands on the
// header and never on the body (T-0152).
const COLLAPSED_HTML = BOARD_HTML.replace(
  '<section data-testid="board-column-ready"',
  `<section data-testid="board-column-someday" class="mm-column" data-section="someday"
            data-collapsed="true" data-count="1">
     <header data-testid="board-column-someday-header" class="mm-column__header">
       <button data-testid="board-column-someday-toggle" class="mm-column__toggle"
               type="button" aria-label="Toggle Someday column">v</button>
       <h2 data-testid="board-column-someday-title" class="mm-column__title">Someday</h2>
     </header>
     <div data-testid="board-column-someday-body" class="mm-column__body" data-column="someday">
       <article data-testid="item-T-0009" class="mm-item" data-item-id="T-0009" tabindex="0">
         <h3><a href="/p/x/item/T-0009">Someday one</a></h3>
       </article>
     </div>
   </section>
   <section data-testid="board-column-ready"`,
);

// The board with an item panel over it, carrying the Wake-up group of §5.6 as
// the server renders it (T-0173). The `hidden` attributes are the server's
// first render — the group hidden until the stage selector names a
// tickler_stages source, and every control but the selected kind's hidden —
// and what these tests drive is mm.js keeping them right afterwards.
//
// panelHTML(kind, stage) builds the panel for a starting state, because both
// visibility rules are about a state CHANGING and the starting point decides
// what a change proves. data-tickler-sources carries "someday", version 1's
// fixed source, same as the real server renders it.
function panelHTML(kind = 'never', stage = 'ready') {
  const hide = (k) => (k === kind ? '' : 'hidden');
  const panel = `
  <aside data-testid="item-panel" class="mm-panel" data-new="no" data-dirty="false">
    <form data-testid="item-form">
      <input data-testid="item-field-title" name="title" value="First">
      <select data-testid="item-field-stage" name="stage">
        <option value="ready" ${stage === 'ready' ? 'selected' : ''}>Ready</option>
        <option value="someday" ${stage === 'someday' ? 'selected' : ''}>Someday</option>
      </select>
      <fieldset data-testid="item-tickler" class="mm-tickler"
                data-present="${kind === 'never' ? 'false' : 'true'}"
                data-tickler-sources="someday"
                ${stage === 'someday' ? '' : 'hidden'}>
        <select data-testid="tickler-kind" name="tickler-kind">
          <option value="never" ${kind === 'never' ? 'selected' : ''}>Never</option>
          <option value="one-time" ${kind === 'one-time' ? 'selected' : ''}>One-time</option>
          <option value="weekly" ${kind === 'weekly' ? 'selected' : ''}>Weekly</option>
          <option value="monthly" ${kind === 'monthly' ? 'selected' : ''}>Monthly</option>
        </select>
        <div class="mm-tickler__control" data-kind="one-time" ${hide('one-time')}>
          <input data-testid="tickler-date" type="date" name="tickler-date">
        </div>
        <div class="mm-tickler__control" data-kind="weekly" ${hide('weekly')}>
          <select data-testid="tickler-weekday" name="tickler-weekday"></select>
          <select data-testid="tickler-ordinal" name="tickler-ordinal"></select>
        </div>
        <div class="mm-tickler__control" data-kind="monthly" ${hide('monthly')}>
          <input data-testid="tickler-monthday" type="text" pattern="(0?[1-9]|[12][0-9]|3[01]|last)" name="tickler-monthday">
        </div>
        <div class="mm-tickler__control" data-kind="time" ${kind === 'never' ? 'hidden' : ''}>
          <input data-testid="tickler-time" type="time" name="tickler-time">
        </div>
      </fieldset>
      <button data-testid="item-save" type="submit">Save</button>
    </form>
    <div data-testid="x-item-freshness" hx-get="/p/x/item/T-0001/freshness?revision=abc"
         hx-trigger="sse:item from:body" hx-swap="morph">
      <a data-testid="x-item-freshness-reload" href="/p/x/item/T-0001">Reload item</a>
    </div>
  </aside>
`;
  return BOARD_HTML.replace('<div data-testid="toast-region"', panel + '  <div data-testid="toast-region"');
}

// htmx double: records every ajax call, and lets a test fire htmx:* events on
// document.body the way the real htmx does (CustomEvent, bubbles).
function makeHtmx(win) {
  const api = {
    calls: [],
    ajax(method, url, opts) {
      api.calls.push({ method, url, opts });
      // commitMove().catch() needs a promise.
      return Promise.resolve();
    },
    fire(name, detail) {
      const evt = new win.CustomEvent(name, {
        bubbles: true, cancelable: true, composed: true, detail,
      });
      win.document.body.dispatchEvent(evt);
      return evt;
    },
  };
  return api;
}

// load runs the real mm.js against a jsdom document, with the htmx double on
// the global scope. mm.js executes in the NODE realm (with jsdom's document,
// navigator, localStorage as globals) rather than inside the jsdom window, so
// its timers are node's and node:test's mock timers can control them.
function load(t, html = BOARD_HTML, opts = {}) {
  const dom = new JSDOM(html, { url: 'http://localhost/p/x/board' });
  const { window: win } = dom;

  global.window = win;
  global.document = win.document;
  // Node 21+ defines globalThis.navigator as a getter; shadow it.
  Object.defineProperty(global, 'navigator', { value: win.navigator, configurable: true });
  global.localStorage = win.localStorage;
  global.HTMLElement = win.HTMLElement;
  global.HTMLInputElement = win.HTMLInputElement;
  global.CustomEvent = win.CustomEvent;
  global.MouseEvent = win.MouseEvent;
  global.KeyboardEvent = win.KeyboardEvent;
  global.Event = win.Event;
  global.Node = win.Node;
  global.Window = win.Window;
  global.MutationObserver = win.MutationObserver;
  global.ResizeObserver = win.ResizeObserver || class { observe() {} unobserve() {} disconnect() {} };
  // CodeMirror schedules layout measurement here. jsdom has no layout, so a
  // never-fired frame is more faithful than letting stale callbacks escape a
  // completed test into the next document.
  global.requestAnimationFrame = () => 0;
  global.cancelAnimationFrame = () => {};
  win.requestAnimationFrame = global.requestAnimationFrame;
  win.cancelAnimationFrame = global.cancelAnimationFrame;

  if (opts.markdownit !== false) win.markdownit = opts.markdownit || MARKDOWN_IT;
  if (opts.codemirror) {
    // Exercise the actual checked-in bundle, not a facsimile of its API.
    (0, eval)(CODEMIRROR_JS);
  }

  if (opts.clipboard) {
    win.navigator.clipboard = opts.clipboard;
  }

  const htmx = makeHtmx(win);
  global.htmx = htmx;
  global.__win = win;

  // eslint-disable-next-line no-eval
  (0, eval)(MM_JS);
  return { win, dom, htmx };
}

function markdownPanelHTML(isNew = false, source = '# Heading\n\nSome **detail**.') {
  const escaped = source.replaceAll('&', '&amp;').replaceAll('<', '&lt;').replaceAll('>', '&gt;');
  const panel = `
  <aside data-testid="item-panel" data-new="${isNew ? 'yes' : 'no'}">
    <form data-testid="item-form">
      <section data-testid="x-item-detail-markdown" data-initial-mode="${isNew ? 'edit' : 'view'}" data-mode="edit">
        <div data-testid="x-item-detail-preview" hidden></div>
        <p data-testid="x-item-detail-empty" hidden>No detail yet.</p>
        <label data-testid="x-item-detail-editor">
          <textarea data-testid="item-field-detail" name="detail">${escaped}</textarea>
        </label>
        <div data-testid="x-item-detail-mode-controls" hidden>
          <button data-testid="x-item-detail-edit" type="button">Edit detail</button>
          <button data-testid="x-item-detail-preview-button" type="button">Preview detail</button>
        </div>
      </section>
      <button data-testid="item-save" type="submit">Save</button>
    </form>
  </aside>`;
  return BOARD_HTML.replace('<div data-testid="toast-region"', panel + '<div data-testid="toast-region"');
}

// Fake dataTransfer, as jsdom events have none (a real drag carries one).
const dragTransfer = () => ({
  effectAllowed: '',
  dropEffect: '',
  setData() {},
});

// A drag event: CustomEvent so we can attach the fake dataTransfer, with the
// coordinates the §7 handlers read.
function dragEvent(win, type, clientY) {
  const evt = new win.CustomEvent(type, { bubbles: true, cancelable: true });
  evt.clientY = clientY;
  Object.defineProperty(evt, 'dataTransfer', { value: dragTransfer(), configurable: true });
  return evt;
}

// Synthetic card geometry: jsdom measures nothing, and the drag index is
// derived from getBoundingClientRect midpoints, so the cards get explicit
// tops. Card i occupies [top, top+height). Column bodies sit at top 0.
function stubLayout(win, height = 40) {
  let top = 0;
  for (const el of win.document.querySelectorAll('.mm-item')) {
    const t = top; // capture THIS card's top; a closure over the live `top`
    // would report the final value for every card.
    el.getBoundingClientRect = () => ({
      top: t, bottom: t + height, height, width: 0, left: 0, right: 0, x: 0, y: t,
      toJSON() {},
    });
    top += height;
  }
  for (const el of win.document.querySelectorAll('.mm-column__body')) {
    el.getBoundingClientRect = () => ({
      top: 0, bottom: 0, height: 0, width: 0, left: 0, right: 0, x: 0, y: 0,
      toJSON() {},
    });
  }
}

const byTestid = (win, id) => win.document.querySelector(`[data-testid="${id}"]`);
const click = (win, el) => el.dispatchEvent(new win.MouseEvent('click', { bubbles: true }));

/* ------------------------------------------------ Markdown detail viewer */

test('existing detail opens rendered and edit-preview preserves unsaved source', (t) => {
  const source = '# Heading\n\nSome **detail**.\n\n<img src=x onerror=alert(1)>\n\n[bad](javascript:alert(1))';
  const { win } = load(t, markdownPanelHTML(false, source));
  const group = byTestid(win, 'x-item-detail-markdown');
  const preview = byTestid(win, 'x-item-detail-preview');
  const editor = byTestid(win, 'x-item-detail-editor');
  const textarea = byTestid(win, 'item-field-detail');

  assert.equal(group.getAttribute('data-mode'), 'view');
  assert.equal(editor.hidden, true);
  assert.ok(preview.querySelector('h1'));
  assert.ok(preview.querySelector('strong'));
  assert.equal(preview.querySelector('img'), null, 'source HTML stays inert');
  assert.equal(preview.querySelector('a'), null, 'unsafe protocols do not become links');

  click(win, byTestid(win, 'x-item-detail-edit'));
  textarea.value = '**unsaved preview**';
  textarea.setSelectionRange(2, 9);
  click(win, byTestid(win, 'x-item-detail-preview-button'));
  assert.equal(group.getAttribute('data-mode'), 'view');
  assert.equal(preview.querySelector('strong').textContent, 'unsaved preview');
  assert.equal(textarea.value, '**unsaved preview**', 'preview does not rewrite source');

  click(win, byTestid(win, 'x-item-detail-edit'));
  assert.equal(group.getAttribute('data-mode'), 'edit');
  assert.equal(textarea.selectionStart, 2);
  assert.equal(textarea.selectionEnd, 9);
});

test('new detail opens in edit mode and parser failure leaves textarea usable', (t) => {
  let loaded = load(t, markdownPanelHTML(true, 'draft'));
  assert.equal(byTestid(loaded.win, 'x-item-detail-markdown').getAttribute('data-mode'), 'edit');
  assert.equal(byTestid(loaded.win, 'x-item-detail-editor').hidden, false);

  loaded = load(t, markdownPanelHTML(false, 'fallback'), { markdownit: false });
  assert.equal(byTestid(loaded.win, 'x-item-detail-editor').hidden, false);
  assert.equal(byTestid(loaded.win, 'x-item-detail-mode-controls').hidden, true);
  assert.equal(byTestid(loaded.win, 'item-field-detail').value, 'fallback');

  loaded = load(t, markdownPanelHTML(false, 'broken'), {
    markdownit: () => ({ render() { throw new Error('parser failed'); } }),
  });
  assert.equal(byTestid(loaded.win, 'x-item-detail-editor').hidden, false);
  assert.equal(byTestid(loaded.win, 'x-item-detail-mode-controls').hidden, true);
});

test('empty and oversized details have bounded safe presentations', (t) => {
  let loaded = load(t, markdownPanelHTML(false, ''));
  assert.equal(byTestid(loaded.win, 'x-item-detail-empty').hidden, false);
  assert.equal(byTestid(loaded.win, 'x-item-detail-preview').hidden, true);

  const large = '*' + 'x'.repeat(524288);
  loaded = load(t, markdownPanelHTML(false, large));
  const preview = byTestid(loaded.win, 'x-item-detail-preview');
  assert.equal(preview.getAttribute('data-render'), 'plain-large');
  assert.equal(preview.textContent, large);
  assert.equal(preview.children.length, 0, 'large fallback inserts no markup');
});

test('real CodeMirror bundle enhances, synchronizes, previews, and tears down', (t) => {
  const loaded = load(t, markdownPanelHTML(true, '# draft'), { codemirror: true });
  const textarea = byTestid(loaded.win, 'item-field-detail');
  const editor = textarea._mmCodeMirror;
  assert.ok(editor, 'real bundle created an editor');
  assert.equal(textarea.hidden, true);
  assert.ok(byTestid(loaded.win, 'x-item-detail-codemirror'));

  let inputEvents = 0;
  textarea.addEventListener('input', () => inputEvents++);
  editor.view.dispatch({changes: {from: 0, to: editor.view.state.doc.length, insert: '**changed**'}});
  assert.equal(textarea.value, '**changed**', 'CodeMirror keeps canonical textarea synchronized');
  assert.equal(inputEvents, 1, 'synchronization follows the ordinary input event path');

  click(loaded.win, byTestid(loaded.win, 'x-item-detail-preview-button'));
  assert.equal(byTestid(loaded.win, 'x-item-detail-preview').querySelector('strong').textContent, 'changed');
  click(loaded.win, byTestid(loaded.win, 'x-item-detail-edit'));
  assert.equal(textarea._mmCodeMirror, editor, 'preview round-trip preserves editor and history');

  loaded.htmx.fire('htmx:beforeCleanupElement', {elt: byTestid(loaded.win, 'item-panel')});
  assert.equal(textarea._mmCodeMirror, undefined);
  assert.equal(textarea.hidden, false);
  assert.equal(byTestid(loaded.win, 'x-item-detail-codemirror'), null);
});

test('existing item: first switch to edit creates a real editor with a finite selection', (t) => {
  // Regression: an existing item opens in view mode with no CodeMirror
  // instance yet, so the pre-edit selection is captured off the plain
  // textarea (an {anchor, head, scrollTop} triple) rather than via
  // editor.capture(). The first click on "Edit detail" then creates the
  // real editor and immediately restores that captured selection into it.
  // Before the fix, the captured value was a [start, end, scroll] array,
  // so `saved.anchor`/`saved.head` were undefined and the restored
  // selection silently became {anchor: NaN, head: NaN} — which is what
  // sent the real bundle into the lineAt/lineBlockAt crash loop the user
  // saw on scroll.
  const loaded = load(t, markdownPanelHTML(false, '# Heading\n\nSome detail.'), {codemirror: true});
  const group = byTestid(loaded.win, 'x-item-detail-markdown');
  assert.equal(group.getAttribute('data-mode'), 'view');

  click(loaded.win, byTestid(loaded.win, 'x-item-detail-edit'));

  const textarea = byTestid(loaded.win, 'item-field-detail');
  const editor = textarea._mmCodeMirror;
  assert.ok(editor, 'real bundle created an editor on first edit');
  const selection = editor.view.state.selection.main;
  assert.ok(Number.isFinite(selection.anchor), 'anchor must not be NaN');
  assert.ok(Number.isFinite(selection.head), 'head must not be NaN');
  assert.equal(selection.anchor, 0);
  assert.equal(selection.head, 0);
});

test('CodeMirror failure and large source retain the native textarea', (t) => {
  let loaded = load(t, markdownPanelHTML(true, 'fallback'));
  loaded.win.mmCodeMirror = {create() { throw new Error('failed'); }};
  // Re-run initialization through an htmx replacement because initial load had
  // no editor factory.
  const group = byTestid(loaded.win, 'x-item-detail-markdown');
  group.removeAttribute('data-markdown-ready');
  loaded.htmx.fire('htmx:afterSwap', {target: group});
  let textarea = byTestid(loaded.win, 'item-field-detail');
  assert.equal(textarea.hidden, false);
  assert.equal(textarea.getAttribute('data-editor'), 'native-error');

  loaded = load(t, markdownPanelHTML(true, 'x'.repeat(262145)), {codemirror: true});
  textarea = byTestid(loaded.win, 'item-field-detail');
  assert.equal(textarea.hidden, false);
  assert.equal(textarea.getAttribute('data-editor'), 'native-large');
  assert.equal(textarea._mmCodeMirror, undefined);
});

/* ------------------------------------------------------------------ busy */

test('data-busy tracks requests in flight and settles to false', (t) => {
  const { win, htmx } = load(t);
  const app = byTestid(win, 'app');

  assert.equal(app.getAttribute('data-busy'), 'false', 'born idle');

  htmx.fire('htmx:beforeRequest', { requestConfig: { verb: 'get' } });
  assert.equal(app.getAttribute('data-busy'), 'true', 'one request in flight');

  htmx.fire('htmx:afterSettle', { requestConfig: { verb: 'get' } });
  assert.equal(app.getAttribute('data-busy'), 'false', 'settled');

  // Two overlapping requests: busy clears only when BOTH settle.
  htmx.fire('htmx:beforeRequest', { requestConfig: { verb: 'get' } });
  htmx.fire('htmx:beforeRequest', { requestConfig: { verb: 'get' } });
  htmx.fire('htmx:afterSettle', { requestConfig: { verb: 'get' } });
  assert.equal(app.getAttribute('data-busy'), 'true', 'one of two still in flight');
  htmx.fire('htmx:afterSettle', { requestConfig: { verb: 'get' } });
  assert.equal(app.getAttribute('data-busy'), 'false', 'both settled');

  // An errored request settles too.
  htmx.fire('htmx:beforeRequest', { requestConfig: { verb: 'get' } });
  htmx.fire('htmx:responseError', { requestConfig: { verb: 'get' } });
  assert.equal(app.getAttribute('data-busy'), 'false');
});

/* ------------------------------------------------------- echo suppression */

test('a board/status refresh echoes nothing within the mutation window', (t) => {
  const { win, htmx } = load(t);

  // A mutation settles on the board…
  htmx.fire('htmx:afterSettle', {
    requestConfig: { verb: 'patch' },
    target: byTestid(win, 'board'),
  });

  // …and the next board or status GET is cancelled (T-0130).
  const boardGet = htmx.fire('htmx:beforeRequest', {
    requestConfig: { verb: 'get' },
    target: byTestid(win, 'board'),
  });
  assert.equal(boardGet.defaultPrevented, true, 'board GET during the echo window is cancelled');

  const statusGet = htmx.fire('htmx:beforeRequest', {
    requestConfig: { verb: 'get' },
    target: byTestid(win, 'app-status'),
  });
  assert.equal(statusGet.defaultPrevented, true, 'status GET during the echo window is cancelled');

  // A GET aimed elsewhere is not the echo.
  const other = htmx.fire('htmx:beforeRequest', {
    requestConfig: { verb: 'get' },
    target: byTestid(win, 'board-column-ready'),
  });
  assert.equal(other.defaultPrevented, false, 'an unrelated GET is not cancelled');
});

/* ---------------------------------------------------------------- menus */

test('an item menu opens, closes, and closes its siblings', (t) => {
  const { win } = load(t);
  const toggle = byTestid(win, 'item-T-0001-menu');
  const menu = byTestid(win, 'item-T-0001-menu-items');

  assert.equal(menu.hidden, true);
  click(win, toggle);
  assert.equal(menu.hidden, false, 'clicking the toggle opens the menu');
  assert.equal(toggle.getAttribute('aria-expanded'), 'true');

  click(win, toggle);
  assert.equal(menu.hidden, true, 'clicking again closes it');
  assert.equal(toggle.getAttribute('aria-expanded'), 'false');
});

test('clicking outside closes every menu', (t) => {
  const { win } = load(t);
  const toggle = byTestid(win, 'item-T-0001-menu');
  const menu = byTestid(win, 'item-T-0001-menu-items');

  click(win, toggle);
  assert.equal(menu.hidden, false);

  click(win, byTestid(win, 'item-T-0002'));
  assert.equal(menu.hidden, true, 'a click on the board closes the menu');
  assert.equal(toggle.getAttribute('aria-expanded'), 'false');
});

test('a menu entry posts its operation through htmx.ajax', (t) => {
  const { win, htmx } = load(t);
  const start = byTestid(win, 'item-T-0001-action-start');

  click(win, start);

  assert.equal(htmx.calls.length, 1);
  const call = htmx.calls[0];
  assert.equal(call.method, 'POST');
  assert.equal(call.url, '/p/x/items/T-0001/start');
  assert.equal(call.opts.target, "[data-testid='board']");
  assert.equal(call.opts.swap, 'morph');
});

test('a dialog op fetches its dialog instead of posting', (t) => {
  const { win, htmx } = load(t);
  click(win, byTestid(win, 'item-T-0001-action-remove'));

  assert.equal(htmx.calls.length, 1);
  const call = htmx.calls[0];
  assert.equal(call.method, 'GET');
  assert.equal(call.url, '/p/x/dialog/confirm-remove?item=T-0001');
  assert.equal(call.opts.target, "[data-testid='dialog-root']");
});

test('panel ops are left to their own hx-get; disabled entries do nothing', (t) => {
  const { win, htmx } = load(t);

  click(win, byTestid(win, 'item-T-0001-action-edit'));
  assert.equal(htmx.calls.length, 0, 'edit opens the panel through the entry\'s own hx-get');

  click(win, byTestid(win, 'item-T-0001-action-fail'));
  assert.equal(htmx.calls.length, 0, 'a disabled entry issues nothing');
});

/* -------------------------------------------------------------- dialogs */

test('Escape closes a dialog and restores focus to its opener', (t) => {
  const { win } = load(t);
  const dialogRoot = byTestid(win, 'dialog-root');
  dialogRoot.innerHTML = '<div class="mm-dialog"><button data-testid="d-inner">ok</button></div>';

  const opener = byTestid(win, 'item-T-0001-menu');
  // The opener is remembered when a request is aimed at the dialog root.
  opener.setAttribute('hx-target', "[data-testid='dialog-root']");
  win.document.body.dispatchEvent(new win.CustomEvent('htmx:beforeRequest', {
    bubbles: true,
    detail: { elt: opener, requestConfig: {} },
  }));

  const dialog = dialogRoot.querySelector('.mm-dialog');
  dialog.dispatchEvent(new win.KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));

  assert.equal(dialogRoot.querySelector('.mm-dialog'), null, 'Escape removes the dialog');
  assert.equal(win.document.activeElement, opener, 'focus returns to the opener');
});

test('the focus trap cycles Tab within the dialog', (t) => {
  const { win } = load(t);
  const dialogRoot = byTestid(win, 'dialog-root');
  dialogRoot.innerHTML =
    '<div class="mm-dialog"><button data-testid="d-a">a</button><button data-testid="d-b">b</button></div>';
  const a = byTestid(win, 'd-a');
  const b = byTestid(win, 'd-b');

  b.focus();
  b.dispatchEvent(new win.KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }));
  assert.equal(win.document.activeElement, a, 'Tab from the last wraps to the first');

  a.focus();
  const shifted = new win.KeyboardEvent('keydown', {
    key: 'Tab', shiftKey: true, bubbles: true, cancelable: true,
  });
  a.dispatchEvent(shifted);
  assert.equal(shifted.defaultPrevented, true, 'Shift+Tab from the first is swallowed');
  assert.equal(win.document.activeElement, b, 'Shift+Tab from the first wraps to the last');
});

/* ------------------------------------------------- the Wake-up group (T-0173) */

/*
  §5.6's two visibility rules. Both are client-side because a select must
  answer instantly, and both are only ever a VIEW: what they reveal is still
  composed and validated server-side on save (§4.2), so nothing here decides
  what a schedule means.
*/

const groupOf = (win) => byTestid(win, 'item-tickler');
const controlFor = (win, kind) =>
  groupOf(win).querySelector(`[data-kind="${kind}"]`);
const change = (win, el) => el.dispatchEvent(new win.Event('change', { bubbles: true }));

test('the kind select shows the matching control and hides the others', (t) => {
  const { win } = load(t, panelHTML('never', 'someday'));
  const kind = byTestid(win, 'tickler-kind');

  // Starting state: kind never, so every control is hidden, the time one too.
  for (const k of ['one-time', 'weekly', 'monthly', 'time']) {
    assert.equal(controlFor(win, k).hidden, true, `${k} starts hidden`);
  }

  kind.value = 'weekly';
  change(win, kind);
  assert.equal(controlFor(win, 'weekly').hidden, false, 'weekly is revealed');
  assert.equal(controlFor(win, 'one-time').hidden, true, 'one-time stays hidden');
  assert.equal(controlFor(win, 'monthly').hidden, true, 'monthly stays hidden');
  assert.equal(controlFor(win, 'time').hidden, false, 'the time control shows for every kind but never');

  kind.value = 'monthly';
  change(win, kind);
  assert.equal(controlFor(win, 'monthly').hidden, false, 'monthly is revealed');
  assert.equal(controlFor(win, 'weekly').hidden, true, 'the previous kind is hidden again');

  kind.value = 'never';
  change(win, kind);
  for (const k of ['one-time', 'weekly', 'monthly', 'time']) {
    assert.equal(controlFor(win, k).hidden, true, `${k} is hidden again by never`);
  }
});

test('the new panel reveals the group when the stage becomes Someday', (t) => {
  const { win } = load(t, panelHTML('never', 'ready'));
  const stage = byTestid(win, 'item-field-stage');

  assert.equal(groupOf(win).hidden, true, 'the selector defaults to Ready, where the group is hidden');

  stage.value = 'someday';
  change(win, stage);
  assert.equal(groupOf(win).hidden, false, 'Someday reveals it without a round trip');

  stage.value = 'ready';
  change(win, stage);
  assert.equal(groupOf(win).hidden, true, 'and switching back hides it again');
});

test('a swap re-reconciles the group with its kind select', (t) => {
  // save-and-add-another replaces the panel wholesale, so the settle handler is
  // what keeps a freshly swapped form's controls matching its own select.
  const { win, htmx } = load(t, panelHTML('never', 'someday'));
  const kind = byTestid(win, 'tickler-kind');

  // A swap lands a form whose select says one-time while the controls still
  // carry the previous render's hidden flags.
  kind.value = 'one-time';
  assert.equal(controlFor(win, 'one-time').hidden, true, 'stale, as a swap can leave it');

  htmx.fire('htmx:afterSettle', { requestConfig: { verb: 'get' } });
  assert.equal(controlFor(win, 'one-time').hidden, false, 'the settle reconciles it');
  assert.equal(controlFor(win, 'time').hidden, false, 'and the time control with it');
});

test('a board with no Wake-up group is left alone', (t) => {
  // The group is absent from a ready or blocked item's panel, and from the
  // board on its own. Neither handler may throw when it finds nothing.
  const { win, htmx } = load(t);
  const section = win.document.createElement('select');
  section.setAttribute('data-testid', 'item-field-section');
  win.document.body.appendChild(section);

  change(win, section);
  htmx.fire('htmx:afterSettle', { requestConfig: { verb: 'get' } });
  assert.equal(byTestid(win, 'item-tickler'), null, 'nothing was invented');
});

/* ---------------------------------------------------------------- toasts */

test('toasts auto-dismiss after six seconds, unless test mode', (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const { win, htmx } = load(t);
  const region = byTestid(win, 'toast-region');
  region.innerHTML = '<div data-testid="toast-1" class="mm-toast">done</div>';

  htmx.fire('htmx:afterSettle', { requestConfig: { verb: 'post' } });
  assert.ok(byTestid(win, 'toast-1'), 'the toast is still there at first');
  assert.equal(byTestid(win, 'toast-1').getAttribute('data-timed'), 'true');

  t.mock.timers.tick(6000);
  assert.equal(byTestid(win, 'toast-1'), null, 'after six seconds it is gone');
});

test('test mode keeps toasts until dismissed', (t) => {
  t.mock.timers.enable({ apis: ['setTimeout'] });
  const { win, htmx } = load(t);
  byTestid(win, 'app').setAttribute('data-test-mode', 'true');
  const region = byTestid(win, 'toast-region');
  region.innerHTML = '<div data-testid="toast-1" class="mm-toast">done</div>';

  htmx.fire('htmx:afterSettle', { requestConfig: { verb: 'post' } });
  t.mock.timers.tick(6000);
  assert.ok(byTestid(win, 'toast-1'), 'test mode does not auto-dismiss (§4.4)');
});

/* ---------------------------------------------------------------- copy */

test('copy puts the data-clipboard text on the clipboard', async (t) => {
  let written = null;
  const { win } = load(t, BOARD_HTML, {
    clipboard: { writeText: (text) => { written = text; return Promise.resolve(); } },
  });
  const button = win.document.createElement('button');
  button.setAttribute('data-clipboard', '- [ ] [T-0001] First');
  button.setAttribute('data-testid', 'x-copy');
  win.document.body.appendChild(button);

  click(win, button);
  await new Promise((resolve) => setImmediate(resolve));

  assert.equal(written, '- [ ] [T-0001] First');
  assert.equal(button.getAttribute('data-copied'), 'true');
});

/* ------------------------------------------------------------ drag & drop */

test('dragstart marks the card and the source column', (t) => {
  const { win } = load(t);
  const card = byTestid(win, 'item-T-0002');
  card.dispatchEvent(dragEvent(win, 'dragstart', 60));

  assert.equal(card.getAttribute('data-dragging'), 'true');
  assert.equal(byTestid(win, 'app').getAttribute('data-drag-source'), 'board-column-ready-body');
});

test('dragover sets the drop index from the pointer, not the card', (t) => {
  const { win } = load(t);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002');
  const body = byTestid(win, 'board-column-ready-body');
  const column = byTestid(win, 'board-column-ready');

  card.dispatchEvent(dragEvent(win, 'dragstart', 60));

  // T-0002 is the dragged card; T-0001 occupies [0,40). clientY=10 is above
  // its midpoint, so the insertion index is 0.
  body.dispatchEvent(dragEvent(win, 'dragover', 10));
  assert.equal(column.getAttribute('data-drop-index'), '0');
  assert.equal(column.getAttribute('data-drop-target'), 'true');
  assert.equal(column.getAttribute('data-drop-allowed'), 'true');

  // Below T-0001's midpoint: index 1 (append).
  body.dispatchEvent(dragEvent(win, 'dragover', 30));
  assert.equal(column.getAttribute('data-drop-index'), '1');

  // A placeholder marks the insertion point, allowed or not.
  assert.ok(win.document.querySelector('[data-testid="drop-placeholder"]'), 'a placeholder is present');
  assert.equal(
    win.document.querySelector('[data-testid="drop-placeholder"]').getAttribute('data-drop-allowed'),
    'true',
  );
});

test('drop commits with the drop\'s own coordinates, not the last dragover', (t) => {
  const { win, htmx } = load(t);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002');
  const body = byTestid(win, 'board-column-ready-body');

  card.dispatchEvent(dragEvent(win, 'dragstart', 60));
  body.dispatchEvent(dragEvent(win, 'dragover', 10)); // index 0…
  body.dispatchEvent(dragEvent(win, 'drop', 30));     // …but the release is at 1

  assert.equal(htmx.calls.length, 1);
  const call = htmx.calls[0];
  assert.equal(call.method, 'POST');
  assert.equal(call.url, '/p/x/items/T-0002/move');
  assert.equal(call.opts.values.position, '2', 'the drop position comes from the release');
  assert.equal(call.opts.values.section, 'ready');
  assert.equal(call.opts.values.stage, 'ready', 'version 2 needs its own field, not just section');
  assert.equal(call.opts.swap, 'morph');
});

test('dragging into a column makes the column the target and clears the others', (t) => {
  const { win } = load(t);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002');
  const ready = byTestid(win, 'board-column-ready');
  const done = byTestid(win, 'board-column-done');
  const doneBody = byTestid(win, 'board-column-done-body');

  card.dispatchEvent(dragEvent(win, 'dragstart', 60));
  ready.setAttribute('data-drop-index', '1');
  doneBody.dispatchEvent(dragEvent(win, 'dragover', 10));

  assert.equal(ready.getAttribute('data-drop-index'), null, 'the old target is cleared');
  assert.equal(done.getAttribute('data-drop-index'), '0');
  assert.equal(done.getAttribute('data-drop-allowed'), 'true');
});

/* ------------------------------------------------- collapsed Someday (T-0152) */

test('a collapsed column is a drop target through its header', (t) => {
  const { win } = load(t, COLLAPSED_HTML);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002');
  const someday = byTestid(win, 'board-column-someday');
  const title = byTestid(win, 'board-column-someday-title');

  card.dispatchEvent(dragEvent(win, 'dragstart', 60));

  // The pointer is over the rotated title — the body is hidden, so this is the
  // only thing a collapsed column offers to aim at.
  const over = dragEvent(win, 'dragover', 10);
  title.dispatchEvent(over);

  assert.equal(over.defaultPrevented, true, 'the dragover is accepted, so a drop can follow');
  assert.equal(someday.getAttribute('data-drop-target'), 'true');
  assert.equal(someday.getAttribute('data-drop-allowed'), 'true');
  // One card already in someday, and a collapsed drop goes to the bottom.
  assert.equal(someday.getAttribute('data-drop-index'), '1', 'collapsed drops land at the bottom');
});

test('dropping on a collapsed column moves the item to the bottom of it', (t) => {
  const { win, htmx } = load(t, COLLAPSED_HTML);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002');
  const header = byTestid(win, 'board-column-someday-header');

  card.dispatchEvent(dragEvent(win, 'dragstart', 60));
  header.dispatchEvent(dragEvent(win, 'dragover', 10));
  header.dispatchEvent(dragEvent(win, 'drop', 10));

  assert.equal(htmx.calls.length, 1, 'the drop posts the move');
  const call = htmx.calls[0];
  assert.equal(call.method, 'POST');
  assert.equal(call.url, '/p/x/items/T-0002/move');
  assert.equal(call.opts.values.section, 'someday');
  assert.equal(call.opts.values.stage, 'someday', 'version 2 needs its own field, not just section');
  assert.equal(call.opts.values.position, '2', 'after the one card already there');
});

test('a keyboard move into a collapsed column also lands at the bottom', (t) => {
  const { win, htmx } = load(t, COLLAPSED_HTML);
  stubLayout(win);
  // T-0001 is FIRST in ready, so the index it carries across is 0 — which is
  // the bottom only if the collapsed rule does nothing.
  const card = byTestid(win, 'item-T-0001');
  const someday = byTestid(win, 'board-column-someday');

  card.dispatchEvent(new win.KeyboardEvent('keydown', { key: ' ', bubbles: true }));
  // someday is the leftmost column, ready the next: one ArrowLeft crosses.
  card.dispatchEvent(new win.KeyboardEvent('keydown', { key: 'ArrowLeft', bubbles: true }));
  assert.equal(someday.getAttribute('data-drop-index'), '1', 'the index is the bottom, not the carried one');

  // Arrows within a collapsed column cannot aim: there is nothing to see.
  card.dispatchEvent(new win.KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }));
  assert.equal(someday.getAttribute('data-drop-index'), '1');

  card.dispatchEvent(new win.KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
  assert.equal(htmx.calls.length, 1);
  assert.equal(htmx.calls[0].opts.values.position, '2');
  assert.equal(htmx.calls[0].opts.values.section, 'someday');
  assert.equal(htmx.calls[0].opts.values.stage, 'someday', 'version 2 needs its own field, not just section');
});

test('an expanded column still takes its index from the pointer', (t) => {
  // The collapsed rule must not leak into the normal case: with
  // data-collapsed="false" the body is in the layout and §7.3's measured index
  // stands.
  const { win } = load(t, COLLAPSED_HTML);
  stubLayout(win);
  const someday = byTestid(win, 'board-column-someday');
  someday.setAttribute('data-collapsed', 'false');
  const body = byTestid(win, 'board-column-someday-body');
  const card = byTestid(win, 'item-T-0002');

  card.dispatchEvent(dragEvent(win, 'dragstart', 60));
  // T-0009 is stubbed at [0,40); a pointer above its midpoint inserts before it.
  body.dispatchEvent(dragEvent(win, 'dragover', 5));
  assert.equal(someday.getAttribute('data-drop-index'), '0');
});

/* ----------------------------------- generic collapse toggle (T-0240) ----------------------------------- */

// Two toggle-bearing columns, the shape every column carries once collapsing
// generalized past Someday: board-column-<key>-toggle, and data-collapsed
// present (and "false") on every column, not only Someday.
const TOGGLE_HTML = `
<div data-testid="app" data-project-id="toggle-project" hx-ext="sse,morph">
  <section data-testid="board" class="mm-board" data-default-collapsed-stages="">
    <section data-testid="board-column-someday" class="mm-column" data-collapsed="false">
      <header data-testid="board-column-someday-header" class="mm-column__header">
        <button data-testid="board-column-someday-toggle" class="mm-column__toggle"
                type="button" aria-label="Toggle Someday column">&gt;</button>
      </header>
      <div data-testid="board-column-someday-body" class="mm-column__body" data-column="someday"></div>
    </section>
    <section data-testid="board-column-ready" class="mm-column" data-collapsed="false">
      <header data-testid="board-column-ready-header" class="mm-column__header">
        <button data-testid="board-column-ready-toggle" class="mm-column__toggle"
                type="button" aria-label="Toggle Ready column">&gt;</button>
      </header>
      <div data-testid="board-column-ready-body" class="mm-column__body" data-column="ready"></div>
    </section>
  </section>
</div>
`;

test('clicking a column toggle collapses it and stores the preference generically', (t) => {
  const { win } = load(t, TOGGLE_HTML);
  const toggle = byTestid(win, 'board-column-ready-toggle');
  const col = byTestid(win, 'board-column-ready');

  click(win, toggle);

  assert.equal(col.getAttribute('data-collapsed'), 'true');
  assert.equal(toggle.textContent, 'v');
  assert.equal(
    win.localStorage.getItem('mm:collapsed-stages:toggle-project'),
    'ready',
    'the ready column, not someday, is what got stored',
  );

  // Clicking again expands it and drops it from the stored set.
  click(win, toggle);
  assert.equal(col.getAttribute('data-collapsed'), 'false');
  assert.equal(toggle.textContent, '>');
  assert.equal(win.localStorage.getItem('mm:collapsed-stages:toggle-project'), '');
});

test('collapsing two columns stores both, comma-separated', (t) => {
  const { win } = load(t, TOGGLE_HTML);
  click(win, byTestid(win, 'board-column-someday-toggle'));
  click(win, byTestid(win, 'board-column-ready-toggle'));

  const stored = win.localStorage.getItem('mm:collapsed-stages:toggle-project').split(',');
  assert.deepEqual(stored.sort(), ['ready', 'someday']);
});

test('the collapsed set is sent as one comma-separated request header', (t) => {
  const { win } = load(t, TOGGLE_HTML);
  click(win, byTestid(win, 'board-column-ready-toggle'));

  const headers = {};
  win.document.body.dispatchEvent(new win.CustomEvent('htmx:configRequest', {
    bubbles: true, detail: { headers },
  }));
  assert.equal(headers['X-Collapsed-Stages'], 'ready');
});

test('a fresh project with no stored preference seeds from the server default', (t) => {
  const html = TOGGLE_HTML
    .replace('data-default-collapsed-stages=""', 'data-default-collapsed-stages="someday"')
    .replace('data-project-id="toggle-project"', 'data-project-id="fresh-project"');
  const { win } = load(t, html);

  win.document.dispatchEvent(new win.Event('DOMContentLoaded', { bubbles: true }));

  const someday = byTestid(win, 'board-column-someday');
  assert.equal(someday.getAttribute('data-collapsed'), 'true', 'seeded collapsed from the default');
  assert.equal(byTestid(win, 'board-column-someday-toggle').textContent, 'v');
  // Ready has no entry in the default, so it stays expanded.
  assert.equal(byTestid(win, 'board-column-ready').getAttribute('data-collapsed'), 'false');
});

/* ------------------------------------------- resizable item panel (T-0242) ------------------------------------------- */

const PANEL_RESIZE_HTML = `
<div data-testid="app" data-project-id="panel-project" hx-ext="sse,morph">
  <aside data-testid="item-panel" class="mm-panel">
    <div data-testid="x-item-panel-resize-handle" class="mm-panel__resize"></div>
  </aside>
</div>
`;

// Real DOM layout metrics (innerWidth, getBoundingClientRect) are all
// jsdom-computed zeros, so this stubs only window.innerWidth - the one input
// the resize math actually reads - rather than faking a full layout engine.
function stubViewportWidth(win, width) {
  Object.defineProperty(win, 'innerWidth', { value: width, configurable: true });
}

test('dragging the resize handle sets the panel width, clamped to the floor and ceiling', (t) => {
  const { win } = load(t, PANEL_RESIZE_HTML);
  stubViewportWidth(win, 1000);
  const handle = byTestid(win, 'x-item-panel-resize-handle');
  const panel = byTestid(win, 'item-panel');

  const down = new win.PointerEvent('pointerdown', { bubbles: true, clientX: 700, pointerId: 1 });
  handle.dispatchEvent(down);
  assert.equal(handle.getAttribute('data-dragging'), 'true');

  // Dragging to x=700 of 1000: width = (1000-700)/1000 = 30%, clamped up to the 33vw floor.
  win.document.dispatchEvent(new win.PointerEvent('pointermove', { bubbles: true, clientX: 700 }));
  assert.equal(panel.style.width, '33vw', 'below the floor clamps to it');

  // Dragging to x=50 of 1000: width would be 95%, clamped down to the 90vw ceiling.
  win.document.dispatchEvent(new win.PointerEvent('pointermove', { bubbles: true, clientX: 50 }));
  assert.equal(panel.style.width, '90vw', 'above the ceiling clamps to it');

  // A mid-range drag is honoured exactly: x=400 of 1000 -> 60vw.
  win.document.dispatchEvent(new win.PointerEvent('pointermove', { bubbles: true, clientX: 400 }));
  assert.equal(panel.style.width, '60vw');

  win.document.dispatchEvent(new win.PointerEvent('pointerup', { bubbles: true, clientX: 400 }));
  assert.equal(handle.getAttribute('data-dragging'), null, 'dragging attribute cleared on release');
  assert.equal(
    win.localStorage.getItem('mm:item-panel-width-vw'), '60',
    'the released width is persisted',
  );
});

test('further pointermove after release does not keep resizing the panel', (t) => {
  const { win } = load(t, PANEL_RESIZE_HTML);
  stubViewportWidth(win, 1000);
  const handle = byTestid(win, 'x-item-panel-resize-handle');
  const panel = byTestid(win, 'item-panel');

  handle.dispatchEvent(new win.PointerEvent('pointerdown', { bubbles: true, clientX: 400, pointerId: 1 }));
  win.document.dispatchEvent(new win.PointerEvent('pointermove', { bubbles: true, clientX: 400 }));
  win.document.dispatchEvent(new win.PointerEvent('pointerup', { bubbles: true, clientX: 400 }));
  assert.equal(panel.style.width, '60vw');

  win.document.dispatchEvent(new win.PointerEvent('pointermove', { bubbles: true, clientX: 100 }));
  assert.equal(panel.style.width, '60vw', 'the drag listener was removed on pointerup');
});

test('a fresh panel is born at the stored width, not the CSS default', (t) => {
  const { win } = load(t, PANEL_RESIZE_HTML);
  win.localStorage.setItem('mm:item-panel-width-vw', '45');

  win.document.dispatchEvent(new win.Event('DOMContentLoaded', { bubbles: true }));

  assert.equal(byTestid(win, 'item-panel').style.width, '45vw');
});

test('a stored width outside the floor/ceiling is clamped on load, not trusted verbatim', (t) => {
  const { win } = load(t, PANEL_RESIZE_HTML);
  win.localStorage.setItem('mm:item-panel-width-vw', '5');

  win.document.dispatchEvent(new win.Event('DOMContentLoaded', { bubbles: true }));

  assert.equal(byTestid(win, 'item-panel').style.width, '33vw');
});

/* -------------------------------------- new-item panel autofocus (T-0252) ------------------------------------------- */

const NEW_ITEM_PANEL_HTML = `
<div data-testid="app" data-project-id="panel-project" hx-ext="sse,morph">
  <aside data-testid="item-panel" data-new="true">
    <input data-testid="item-field-title" name="title" type="text">
  </aside>
</div>
`;

const EXISTING_ITEM_PANEL_HTML = `
<div data-testid="app" data-project-id="panel-project" hx-ext="sse,morph">
  <aside data-testid="item-panel" data-new="false">
    <input data-testid="item-field-title" name="title" type="text" value="Existing">
  </aside>
</div>
`;

test('the new-item panel focuses its title field on load', (t) => {
  const { win } = load(t, NEW_ITEM_PANEL_HTML);
  win.document.dispatchEvent(new win.Event('DOMContentLoaded', { bubbles: true }));
  assert.equal(win.document.activeElement, byTestid(win, 'item-field-title'));
});

test('the new-item panel focuses its title field after an htmx swap', (t) => {
  const { win } = load(t, EXISTING_ITEM_PANEL_HTML);
  const title = byTestid(win, 'item-field-title');
  assert.notEqual(win.document.activeElement, title, 'sanity: not focused yet');

  // Swap the panel out for a fresh new-item one, as board.html's add link
  // (hx-target="#item-panel-root") does.
  byTestid(win, 'item-panel').outerHTML = NEW_ITEM_PANEL_HTML.trim();
  win.document.body.dispatchEvent(new win.CustomEvent('htmx:afterSwap', { bubbles: true }));

  assert.equal(win.document.activeElement, byTestid(win, 'item-field-title'));
});

test('an existing item panel does not steal focus on load', (t) => {
  const { win } = load(t, EXISTING_ITEM_PANEL_HTML);
  win.document.dispatchEvent(new win.Event('DOMContentLoaded', { bubbles: true }));
  assert.notEqual(win.document.activeElement, byTestid(win, 'item-field-title'));
});

test('dragging into done prompts the finish dialog', (t) => {
  const { win, htmx } = load(t);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002');
  const doneBody = byTestid(win, 'board-column-done-body');

  card.dispatchEvent(dragEvent(win, 'dragstart', 60));
  doneBody.dispatchEvent(dragEvent(win, 'dragover', 10));
  doneBody.dispatchEvent(dragEvent(win, 'drop', 10));

  assert.equal(htmx.calls.length, 1);
  const call = htmx.calls[0];
  assert.equal(call.method, 'GET');
  assert.equal(call.url, '/p/x/dialog/finish?item=T-0002', 'finish prompts before it runs (§7.2)');
});

// T-0250: the block dialog fetch carries the drop's own position, so the
// dialog's later submission can land the card where it was actually
// dropped instead of wherever the server's own default would otherwise
// put it.
test('dragging into blocked prompts the block dialog with the drop position', (t) => {
  const html = BOARD_HTML.replace(
    '<section data-testid="board-column-done"',
    `<section data-testid="board-column-blocked" class="mm-column">
       <div data-testid="board-column-blocked-body" class="mm-column__body" data-column="blocked">
         <article data-testid="item-T-0004" class="mm-item" data-item-id="T-0004" tabindex="0">
           <h3><a href="/p/x/item/T-0004">Already blocked</a></h3>
         </article>
       </div>
     </section>
     <section data-testid="board-column-done"`,
  );
  const { win, htmx } = load(t, html);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002'); // ready -> blocked, at the top
  const blockedBody = byTestid(win, 'board-column-blocked-body');

  card.dispatchEvent(dragEvent(win, 'dragstart', 60));
  blockedBody.dispatchEvent(dragEvent(win, 'dragover', 10)); // above the one existing card
  blockedBody.dispatchEvent(dragEvent(win, 'drop', 10));

  assert.equal(htmx.calls.length, 1);
  const call = htmx.calls[0];
  assert.equal(call.method, 'GET');
  assert.equal(call.url, '/p/x/dialog/block?item=T-0002&position=1');
});

test('an illegal drop issues no request', (t) => {
  const { win, htmx } = load(t);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002');
  const doneBody = byTestid(win, 'board-column-done-body');
  const readyBody = byTestid(win, 'board-column-ready-body');

  // Reopening a done item is illegal (spec-gui.md §7.2) - move the card
  // into the done body first so its own column reads done at dragstart.
  // (T-0248/T-0249: working -> working stopped being illegal - a reorder
  // within working is now a plain legal move - so this test needs a
  // transition that is still genuinely refused.)
  doneBody.appendChild(card);
  card.dispatchEvent(dragEvent(win, 'dragstart', 60));
  readyBody.dispatchEvent(dragEvent(win, 'dragover', 10));
  assert.equal(byTestid(win, 'board-column-ready').getAttribute('data-drop-allowed'), 'false');
  readyBody.dispatchEvent(dragEvent(win, 'drop', 10));

  assert.equal(htmx.calls.length, 0, 'a Conflict drop posts nothing');
});

/* ------------------------------------------- a custom stage's legality (T-0248) ------------------------------------------- */

// A stage beyond the four version-1 names (ready/blocked/someday/working) -
// a fixture stands in for a directory's own custom "review" stage.
const REVIEW_HTML = BOARD_HTML.replace(
  '<section data-testid="board-column-ready"',
  `<section data-testid="board-column-review" class="mm-column" data-stage="review" data-needs-reason="false">
     <div data-testid="board-column-review-body" class="mm-column__body" data-column="review">
       <article data-testid="item-T-0009" class="mm-item" data-item-id="T-0009" tabindex="0">
         <h3><a href="/p/x/item/T-0009">In review</a></h3>
       </article>
     </div>
   </section>
   <section data-testid="board-column-ready"`,
);

test('a custom stage accepts a drop from elsewhere on the board', (t) => {
  const { win, htmx } = load(t, REVIEW_HTML);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0001'); // starts in ready
  const reviewBody = byTestid(win, 'board-column-review-body');

  card.dispatchEvent(dragEvent(win, 'dragstart', 60));
  reviewBody.dispatchEvent(dragEvent(win, 'dragover', 10));
  assert.equal(
    byTestid(win, 'board-column-review').getAttribute('data-drop-allowed'), 'true',
    'a stage the four hardcoded names do not include must still accept a drop',
  );
  reviewBody.dispatchEvent(dragEvent(win, 'drop', 10));

  assert.equal(htmx.calls.length, 1, 'the drop posts the move');
  const call = htmx.calls[0];
  assert.equal(call.url, '/p/x/items/T-0001/move');
  assert.equal(call.opts.values.stage, 'review');
  // "review" is not a valid version-1 section (mm.ParseSection), unlike
  // ready/blocked/someday - the field must be omitted, not sent and left
  // for the server to reject (T-0248's second bug, found moments after
  // the first: sending it unconditionally 400'd every drag into or within
  // a custom stage before "stage" was ever read).
  assert.equal(call.opts.values.section, undefined, 'section has no meaning for a custom stage');
});

test('reordering within a custom stage is legal', (t) => {
  const { win, htmx } = load(t, REVIEW_HTML);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0009');
  const reviewBody = byTestid(win, 'board-column-review-body');

  card.dispatchEvent(dragEvent(win, 'dragstart', 60));
  reviewBody.dispatchEvent(dragEvent(win, 'dragover', 10));
  assert.equal(byTestid(win, 'board-column-review').getAttribute('data-drop-allowed'), 'true');
  reviewBody.dispatchEvent(dragEvent(win, 'drop', 10));

  assert.equal(htmx.calls.length, 1);
  assert.equal(htmx.calls[0].url, '/p/x/items/T-0009/move');
});

// start/pause are legal to or from a custom stage too, not only the four
// hardcoded ones - startV2/pauseV2 place no restriction on the OTHER side.
test('start and pause are legal between a custom stage and working', (t) => {
  const html = REVIEW_HTML.replace(
    '<section data-testid="board-column-done"',
    `<section data-testid="board-column-working" class="mm-column">
       <div data-testid="board-column-working-body" class="mm-column__body" data-column="working"></div>
     </section>
     <section data-testid="board-column-done"`,
  );
  const { win, htmx } = load(t, html);
  stubLayout(win);

  const card = byTestid(win, 'item-T-0009'); // review -> working
  const workingBody = byTestid(win, 'board-column-working-body');
  card.dispatchEvent(dragEvent(win, 'dragstart', 60));
  workingBody.dispatchEvent(dragEvent(win, 'dragover', 10));
  workingBody.dispatchEvent(dragEvent(win, 'drop', 10));
  assert.equal(htmx.calls.length, 1);
  assert.equal(htmx.calls[0].url, '/p/x/items/T-0009/start', 'entering working is a start, from any stage');
});

// spec-gui.md §7.2 (T-0249): version 2's working is an ordinary declared
// stage with its own ordered run, not version 1's separate, order-free
// slot files - a reorder within it is a plain --move, same as any other
// stage, not the illegal working-to-working transition it used to be.
test('reordering within working is legal', (t) => {
  const html = BOARD_HTML.replace(
    '<section data-testid="board-column-done"',
    `<section data-testid="board-column-working" class="mm-column">
       <div data-testid="board-column-working-body" class="mm-column__body" data-column="working">
         <article data-testid="item-T-0010" class="mm-item" data-item-id="T-0010" tabindex="0">
           <h3><a href="/p/x/item/T-0010">Working one</a></h3>
         </article>
         <article data-testid="item-T-0011" class="mm-item" data-item-id="T-0011" tabindex="0">
           <h3><a href="/p/x/item/T-0011">Working two</a></h3>
         </article>
       </div>
     </section>
     <section data-testid="board-column-done"`,
  );
  const { win, htmx } = load(t, html);
  stubLayout(win);

  const card = byTestid(win, 'item-T-0010');
  const workingBody = byTestid(win, 'board-column-working-body');
  const workingColumn = byTestid(win, 'board-column-working');

  card.dispatchEvent(dragEvent(win, 'dragstart', 60));
  const other = byTestid(win, 'item-T-0011').getBoundingClientRect();
  const overEvt = dragEvent(win, 'dragover', other.bottom + 5);
  workingBody.dispatchEvent(overEvt);
  assert.equal(overEvt.defaultPrevented, true);
  assert.equal(workingColumn.getAttribute('data-drop-allowed'), 'true',
    'working -> working must no longer be marked Conflict');
  workingBody.dispatchEvent(dragEvent(win, 'drop', other.bottom + 5));

  assert.equal(htmx.calls.length, 1, 'the reorder posts a plain move');
  const call = htmx.calls[0];
  assert.equal(call.url, '/p/x/items/T-0010/move');
  assert.equal(call.opts.values.stage, 'working');
  assert.equal(call.opts.values.section, undefined, '"working" is not a version-1 section');
});

test('dragend clears every drag attribute', (t) => {
  const { win } = load(t);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002');
  const body = byTestid(win, 'board-column-ready-body');
  const column = byTestid(win, 'board-column-ready');

  card.dispatchEvent(dragEvent(win, 'dragstart', 60));
  body.dispatchEvent(dragEvent(win, 'dragover', 10));
  card.dispatchEvent(dragEvent(win, 'dragend', 10));

  assert.equal(card.getAttribute('data-dragging'), null);
  assert.equal(byTestid(win, 'app').getAttribute('data-drag-source'), null);
  assert.equal(column.getAttribute('data-drop-index'), null);
  assert.equal(win.document.querySelector('[data-testid="drop-placeholder"]'), null);
});

/* ---------------------------------------------------------- keyboard moves */

test('keyboard move mode: space enters, arrows move, enter commits', (t) => {
  const { win, htmx } = load(t);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002');
  const body = byTestid(win, 'board-column-ready-body');
  const column = byTestid(win, 'board-column-ready');

  card.dispatchEvent(new win.KeyboardEvent('keydown', { key: ' ', bubbles: true }));
  assert.equal(card.getAttribute('data-move-mode'), 'true', 'space enters move mode');
  assert.equal(column.getAttribute('data-drop-index'), '1', 'begins at the card\'s index');

  card.dispatchEvent(new win.KeyboardEvent('keydown', { key: 'ArrowUp', bubbles: true }));
  assert.equal(column.getAttribute('data-drop-index'), '0', 'arrow up moves the index');

  card.dispatchEvent(new win.KeyboardEvent('keydown', { key: 'Enter', bubbles: true }));
  assert.equal(htmx.calls.length, 1, 'enter commits');
  assert.equal(htmx.calls[0].url, '/p/x/items/T-0002/move');
  assert.equal(htmx.calls[0].opts.values.position, '1');
  assert.equal(card.getAttribute('data-move-mode'), null, 'commit ends move mode');
});

test('keyboard move mode: escape cancels and restores focus', (t) => {
  const { win } = load(t);
  stubLayout(win);
  const card = byTestid(win, 'item-T-0002');

  card.dispatchEvent(new win.KeyboardEvent('keydown', { key: ' ', bubbles: true }));
  const esc = new win.KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true });
  card.dispatchEvent(esc);

  assert.equal(esc.defaultPrevented, true);
  assert.equal(card.getAttribute('data-move-mode'), null);
  assert.equal(win.document.activeElement, card, 'focus returns to the card');
});

/* ---------------------------------------------------- SSE-down polling (T-0139) */

test('the stream closing starts the 5s poll; reopening stops it', (t) => {
  t.mock.timers.enable({ apis: ['setInterval'] });
  const { win, htmx } = load(t);

  htmx.fire('htmx:sseError');
  assert.equal(htmx.calls.length, 2, 'polling converges immediately on stream-down');
  const urls = htmx.calls.map((c) => c.url).sort();
  assert.deepEqual(urls, ['/p/x/board?fragment=1', '/p/x/status']);
  for (const call of htmx.calls) {
    assert.equal(call.method, 'GET');
    assert.equal(call.opts.swap, 'morph', 'polls morph their own region');
  }

  t.mock.timers.tick(5000);
  assert.equal(htmx.calls.length, 4, 'one interval tick polls both regions again');

  htmx.fire('htmx:sseOpen');
  t.mock.timers.tick(15000);
  assert.equal(htmx.calls.length, 4, 'reopening the stream stops the poll for good');
});

test('sseClose starts the poll; a second close does not stack intervals', (t) => {
  t.mock.timers.enable({ apis: ['setInterval'] });
  const { win, htmx } = load(t);

  htmx.fire('htmx:sseClose');
  htmx.fire('htmx:sseClose');
  htmx.fire('htmx:sseError');
  assert.equal(htmx.calls.length, 2, 'every close/error is guarded by one interval');

  t.mock.timers.tick(5000);
  assert.equal(htmx.calls.length, 4, 'exactly one interval runs');
});

test('the theme shell is not polled', (t) => {
  t.mock.timers.enable({ apis: ['setInterval'] });
  const { win, htmx } = load(t);

  // The app root self-refreshes the whole shell on sse:theme with outerHTML.
  // It must NOT join the poll loop: a full app re-render every 5s is the very
  // waste T-0139 exists to avoid.
  byTestid(win, 'app').setAttribute('hx-trigger', 'sse:theme from:body');
  byTestid(win, 'app').setAttribute('hx-get', '/p/x/shell');

  htmx.fire('htmx:sseError');
  t.mock.timers.tick(5000);

  assert.ok(htmx.calls.every((c) => c.url !== '/p/x/shell'), 'the shell is not polled');
  assert.equal(htmx.calls.length, 4, 'only the board and status regions poll');
});

test('item freshness polling preserves input and dirty reload asks first', (t) => {
  t.mock.timers.enable({ apis: ['setInterval'] });
  const { win, htmx } = load(t, panelHTML());
  const title = byTestid(win, 'item-field-title');
  title.value = 'local edit';
  title.dispatchEvent(new win.Event('input', { bubbles: true }));
  assert.equal(byTestid(win, 'item-panel').getAttribute('data-dirty'), 'true');

  title.value = 'First';
  title.dispatchEvent(new win.Event('input', { bubbles: true }));
  assert.equal(byTestid(win, 'item-panel').getAttribute('data-dirty'), 'false',
    'reverting to the rendered values makes the form clean');
  title.value = 'local edit';
  title.dispatchEvent(new win.Event('input', { bubbles: true }));

  htmx.fire('htmx:sseError');
  assert.ok(htmx.calls.some((c) => c.url.includes('/freshness?revision=abc')),
    'SSE-down polling includes the targeted item probe');
  assert.equal(title.value, 'local edit', 'polling does not replace the form');

  win.confirm = () => false;
  const click = new win.MouseEvent('click', { bubbles: true, cancelable: true });
  byTestid(win, 'x-item-freshness-reload').dispatchEvent(click);
  assert.equal(click.defaultPrevented, true, 'declining confirmation preserves local input');

  const freshness = byTestid(win, 'x-item-freshness');
  freshness.setAttribute('data-state', 'changed');
  title.focus();
  htmx.fire('htmx:afterSwap', { target: freshness });
  htmx.fire('htmx:afterSwap', { target: freshness });
  assert.equal(byTestid(win, 'item-save').disabled, true, 'stale save is disabled as guidance');
  assert.equal(win.document.activeElement, title, 'repeated notices do not steal focus');
  assert.equal(title.value, 'local edit', 'repeated notices preserve local input');
});
