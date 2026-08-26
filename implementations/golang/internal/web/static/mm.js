/*
  micro-manager — the only hand-written JavaScript in the project.

  It contains three things and no domain logic beyond one deliberate exception:

  1. data-busy tracking (spec-gui.md §5.1) — the quiescence signal the external
     suite waits on.
  2. Menus and dialogs: opening, closing, and trapping focus (§5.10, §11 rule 5).
  3. Drag and drop with its keyboard equivalent (§7) — added by T-0059.

  The exception is the static transition table of §7.2, replicated so hover
  feedback is instant. §2.1 permits it explicitly, provided the server's answer
  stays authoritative: the table decides what the cursor looks like, and the
  server decides whether the drop happens.

  Keep this file small. If it grows past a few hundred lines, something belongs
  on the server that has drifted onto the client.
*/
(function () {
  'use strict';

  const root = () => document.querySelector('[data-testid="app"]');
  const dialogRoot = () => document.querySelector('[data-testid="dialog-root"]');

  /* ------------------------------------------------------ refresh echoes */

  /*
    T-0130. Two reasons to drop a would-be board/status refresh before it
    ever becomes a request:

    1. Echo. A mutation's response already carries the fresh board (and, via
       OOB, the fresh status); within ~5s the broker's next poll notices the
       new fingerprint and republishes it, and the refresh that follows
       (sse:board/status, or the 30s backstop on unlucky timing) would fetch
       and morph in the IDENTICAL bytes. The broker cannot tell who wrote —
       it only sees a fingerprint change — but the tab that issued the
       write can, so the window is tracked here.
    2. Drag. A refresh mid-gesture would morph in a board that has never
       heard of data-dragging/data-move-mode (the server never emits them),
       stripping them mid-drag and killing the move (move, below in §7).

    Cancelling in htmx:beforeRequest — rather than letting the request run
    and discarding the response — is what keeps data-busy correct: a
    request that never starts never needs to be counted as in flight, and
    nothing would ever settle to decrement it back. This listener is
    registered in the CAPTURE phase specifically so it runs before the
    data-busy listener below and can stop it with stopImmediatePropagation
    before that listener increments inFlight for a request this cancels.
  */
  const ECHO_WINDOW_MS = 1500;
  let lastMutationSettledAt = 0;

  document.body.addEventListener('htmx:beforeRequest', (event) => {
    const cfg = event.detail && event.detail.requestConfig;
    const tgt = event.detail && event.detail.target;
    if (!cfg || !tgt || !tgt.getAttribute || cfg.verb !== 'get') return;

    const testid = tgt.getAttribute('data-testid');
    if (testid !== 'board' && testid !== 'app-status') return;

    const echoing = lastMutationSettledAt !== 0 && (Date.now() - lastMutationSettledAt) < ECHO_WINDOW_MS;
    if (move !== null || echoing) {
      event.preventDefault();
      event.stopImmediatePropagation();
    }
  }, true);

  document.body.addEventListener('htmx:afterSettle', (event) => {
    const cfg = event.detail && event.detail.requestConfig;
    const tgt = event.detail && event.detail.target;
    if (cfg && cfg.verb !== 'get' && tgt && tgt.getAttribute &&
        tgt.getAttribute('data-testid') === 'board') {
      lastMutationSettledAt = Date.now();
    }
  });

  /* ------------------------------------------------------------ data-busy */

  /*
    data-busy is true while ANY request is in flight and false only when the
    view fully reflects server state (§5.1). Requests are counted rather than
    flagged: two overlapping swaps would otherwise clear it while one is still
    running, and every test that waits on quiescence would race.
  */
  let inFlight = 0;

  function setBusy() {
    const app = root();
    if (app) app.setAttribute('data-busy', inFlight > 0 ? 'true' : 'false');
  }

  document.body.addEventListener('htmx:beforeRequest', () => {
    inFlight += 1;
    setBusy();
  });

  for (const event of ['htmx:afterSettle', 'htmx:responseError', 'htmx:sendError', 'htmx:timeout']) {
    document.body.addEventListener(event, () => {
      inFlight = Math.max(0, inFlight - 1);
      setBusy();
    });
  }

  /*
    htmx's default response handling swaps nothing for 4xx/5xx responses
    (responseHandling: "[45].." => swap:false). The server's error responses
    for htmx callers ARE the toast/dialog fragment, carried out of band — so
    without this, a rejected save (a space in a tag, a bad date, a WIP limit)
    fails SILENTLY: the form stays, no toast, no code. Re-enable the swap for
    error bodies that carry OOB elements, but only the OOB part: the in-band
    swap is overridden to "none" so a 400 can never replace the board with
    an error fragment. The OOB then lands the toast in toast-region or the
    dialog in dialog-root, exactly as a 2xx response would.
  */
  document.body.addEventListener('htmx:beforeSwap', (event) => {
    const detail = event.detail;
    if (!detail || !detail.isError || !detail.serverResponse) return;
    if (!detail.serverResponse.includes('hx-swap-oob')) return;
    detail.shouldSwap = true;
    const region = document.querySelector("[data-testid='toast-region']");
    if (region) detail.target = region;
    detail.swapOverride = 'none';
  });

  /* ---------------------------------------------------------------- menus */

  /*
    An item's action menu. The button carries aria-expanded, which is the state
    an assistive technology reads and a test can assert without measuring
    anything.
  */
  function closeMenus(except) {
    document.querySelectorAll('[data-testid$="-menu-items"]').forEach((menu) => {
      if (menu === except) return;
      menu.hidden = true;
      const button = menu.previousElementSibling;
      if (button && button.hasAttribute('aria-expanded')) {
        button.setAttribute('aria-expanded', 'false');
      }
    });
  }

  document.addEventListener('click', (event) => {
    const toggle = event.target.closest('[data-testid$="-menu"], [data-testid="project-switcher"]');

    if (toggle && toggle.getAttribute('data-testid') === 'project-switcher') {
      const menu = document.querySelector('[data-testid="project-switcher-menu"]');
      const open = menu.hidden;
      menu.hidden = !open;
      toggle.setAttribute('aria-expanded', String(open));
      return;
    }

    if (toggle) {
      const menu = toggle.nextElementSibling;
      if (menu) {
        const open = menu.hidden;
        closeMenus(menu);
        menu.hidden = !open;
        toggle.setAttribute('aria-expanded', String(open));
      }
      return;
    }

    if (event.target.closest('[data-dismiss]')) {
      const dialog = event.target.closest('.mm-dialog');
      const toast = event.target.closest('.mm-toast');
      if (dialog) closeDialog(dialog);
      else if (toast) toast.remove();
      return;
    }

    if (!event.target.closest('.mm-item__actions, .mm-dialog, [data-testid="project-switcher-menu"]')) {
      closeMenus(null);
      const switcher = document.querySelector('[data-testid="project-switcher-menu"]');
      if (switcher) {
        switcher.hidden = true;
        document.querySelector('[data-testid="project-switcher"]')
          ?.setAttribute('aria-expanded', 'false');
      }
    }
  });

  /* -------------------------------------------------------------- dialogs */

  /*
    §11 rule 5: dialogs trap focus and restore it to the invoking element on
    close. The invoking element is remembered here rather than guessed on close,
    because by then the button may have been replaced by a swap.
  */
  let dialogOpener = null;

  document.body.addEventListener('htmx:beforeRequest', (event) => {
    const target = event.detail && event.detail.elt;
    if (target && target.getAttribute && target.getAttribute('hx-target') === "[data-testid='dialog-root']") {
      dialogOpener = target;
    }
  });

  document.body.addEventListener('htmx:afterSettle', () => {
    const dialog = dialogRoot() && dialogRoot().querySelector('.mm-dialog');
    if (dialog) focusFirst(dialog);
  });

  function focusFirst(dialog) {
    const focusable = dialog.querySelector(
      'input:not([type=hidden]), select, textarea, button, [href]'
    );
    if (focusable) focusable.focus();
  }

  function closeDialog(dialog) {
    dialog.remove();
    if (dialogOpener && document.contains(dialogOpener)) {
      dialogOpener.focus();
    }
    dialogOpener = null;
  }

  document.addEventListener('keydown', (event) => {
    if (event.key !== 'Escape') return;

    const dialog = dialogRoot() && dialogRoot().querySelector('.mm-dialog');
    if (dialog) {
      closeDialog(dialog);
      return;
    }
    closeMenus(null);
  });

  /* Focus trap: Tab cycles within an open dialog rather than escaping behind it. */
  document.addEventListener('keydown', (event) => {
    if (event.key !== 'Tab') return;
    const dialog = dialogRoot() && dialogRoot().querySelector('.mm-dialog');
    if (!dialog) return;

    const focusable = Array.from(
      dialog.querySelectorAll('input:not([type=hidden]), select, textarea, button, [href]')
    ).filter((el) => !el.disabled);
    if (focusable.length === 0) return;

    const first = focusable[0];
    const last = focusable[focusable.length - 1];
    if (event.shiftKey && document.activeElement === first) {
      event.preventDefault();
      last.focus();
    } else if (!event.shiftKey && document.activeElement === last) {
      event.preventDefault();
      first.focus();
    }
  });

  /* ----------------------------------------------------------- item menus */

  /*
    A menu entry posts its operation. The disabled ones carry data-reason and do
    nothing at all — the server would refuse them with the same code, and issuing
    a request that is known to fail would only make the failure slower.
  */
  document.addEventListener('click', (event) => {
    const button = event.target.closest('.mm-item__action');
    if (!button || button.disabled) return;

    const op = button.getAttribute('data-op');
    const id = button.getAttribute('data-item');
    const app = root();
    if (!op || !id || !app) return;

    const project = app.getAttribute('data-project-id');
    if (!project) return;

    // The three operations that prompt first (§7.2). Everything else posts.
    if (op === 'remove' || op === 'block' || op === 'finish') {
      const name = op === 'remove' ? 'confirm-remove' : op;
      dialogOpener = button;
      htmx.ajax('GET', `/p/${project}/dialog/${name}?item=${id}`, {
        target: "[data-testid='dialog-root']",
      });
      return;
    }
    // The panel owns these, and the entry carries its own hx-get to open it
    // (item-card.html). Returning here leaves htmx to do that work; it does
    // NOT mean "do nothing", which is what this line used to amount to before
    // the entry had those attributes.
    //
    // note is one of them: it needs text, and item-notes in the panel is where
    // the text is typed (§7.2). Posting it from here sent an empty body, so
    // the entry could only ever answer "a note needs some text".
    if (op === 'edit' || op === 'move' || op === 'note') return;

    htmx.ajax('POST', `/p/${project}/items/${id}/${op}`, {
      target: "[data-testid='board']",
      /* morph, not outerHTML (T-0161): diff the fresh board into the old
         one instead of tearing every card down and rebuilding it (the
         flicker source the /new and /item report was about). T-0138 made
         this outerHTML to stop the board's `every 30s` poll chain leaking
         per mutation; T-0139 removed every `every` clause, so the leak is
         impossible and the teardown is pure cost. */
      swap: 'morph',
      // source anchors the morph extension lookup: without it htmx resolves
      // no extension for this request at all (verified empirically) and a
      // "morph" swap silently falls back to innerHTML, nesting the response
      // inside the board instead of replacing it.
      source: app,
    });
  });

  /* ---------------------------------------------------------------- copy */

  /*
    §5.7: report-copy puts the paste-ready markdown on the clipboard. The text
    comes from the DOM rather than from a second render, so what is copied is
    what the server produced - and what the CLI produces, since both call the
    library's one renderer.
  */
  document.addEventListener('click', (event) => {
    const button = event.target.closest('[data-clipboard]');
    if (!button) return;

    const text = button.getAttribute('data-clipboard');
    const done = () => {
      button.setAttribute('data-copied', 'true');
      setTimeout(() => button.removeAttribute('data-copied'), 2000);
    };

    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(done, () => fallbackCopy(text, done));
      return;
    }
    fallbackCopy(text, done);
  });

  /* Clipboard access can be refused, and a copy button that silently does
     nothing is worse than one that uses the old API. */
  function fallbackCopy(text, done) {
    const area = document.createElement('textarea');
    area.value = text;
    area.setAttribute('readonly', '');
    area.style.position = 'absolute';
    area.style.left = '-9999px';
    document.body.appendChild(area);
    area.select();
    try {
      document.execCommand('copy');
      done();
    } finally {
      area.remove();
    }
  }

  /* Toasts persist in test mode until dismissed (§4.4 rule 2). */
  document.body.addEventListener('htmx:afterSettle', () => {
    const app = root();
    if (!app || app.getAttribute('data-test-mode') === 'true') return;
    document.querySelectorAll('.mm-toast:not([data-timed])').forEach((toast) => {
      toast.setAttribute('data-timed', 'true');
      setTimeout(() => toast.remove(), 6000);
    });
  });

  /* --------------------------------------------------------- tickler */

  /*
    §5.6: the Wake-up group. Two visibility rules, both client-side so the
    form responds without a round trip:

      - the kind select shows the matching control and hides the others; the
        time control shows for every kind but never;
      - in the NEW panel the group is hidden until the stage selector names a
        tickler_stages source — the server renders it hidden, this reveals
        it. data-tickler-sources carries the eligible slugs (version 1's
        fixed "someday", or version 2's declared sources), so this reads the
        directory's own declaration rather than assuming "someday".

    The server's answers stay authoritative: what these toggles reveal is
    still composed and validated server-side on save (§4.2).
  */
  function applyTicklerGroup(group) {
    if (!group) return;
    const kindSelect = group.querySelector('[data-testid="tickler-kind"]');
    const selected = kindSelect ? kindSelect.value : 'never';
    group.querySelectorAll('[data-kind]').forEach((control) => {
      const kind = control.getAttribute('data-kind');
      control.hidden = kind === 'time' ? selected === 'never' : kind !== selected;
    });
  }

  function applyTicklerStage(form) {
    if (!form) return;
    const group = form.querySelector('[data-testid="item-tickler"]');
    const stage = form.querySelector('[data-testid="item-field-stage"]');
    if (!group || !stage) return;
    const sources = (group.getAttribute('data-tickler-sources') || '').split(/\s+/).filter(Boolean);
    group.hidden = !sources.includes(stage.value);
  }

  document.addEventListener('change', (event) => {
    const target = event.target;
    if (!target || !target.getAttribute) return;
    if (target.getAttribute('data-testid') === 'tickler-kind') {
      applyTicklerGroup(target.closest('[data-testid="item-tickler"]'));
    }
    if (target.getAttribute('data-testid') === 'item-field-stage') {
      applyTicklerStage(target.closest('form'));
    }
  });

  /* A swap can replace the panel wholesale (save-and-add-another re-opens a
     fresh form), so after every settle the group's controls are re-reconciled
     with its kind select. */
  document.body.addEventListener('htmx:afterSettle', () => {
    document.querySelectorAll('[data-testid="item-tickler"]').forEach(applyTicklerGroup);
  });

  /* ================================================================ §7 */

  /*
    Drag and drop, and the keyboard move mode.

    §7.4 is the reason these are one mechanism rather than two: "Move mode MUST
    maintain the same data-drop-* attributes as a pointer drag, so one set of
    test assertions covers both input paths." Everything below therefore goes
    through beginMove / hoverTarget / commitMove, and the pointer handlers and
    the key handlers are two thin ways in.
  */

  /*
    The transition table of §7.2, replicated here so hover feedback is instant.

    This is the ONE domain rule the client is allowed to hold (§2.1), and only
    because the server's answer stays authoritative: the table decides what the
    cursor looks like, and the server decides whether the drop happens. The
    error codes are the ones the server would return, so the two agree.
  */
  // The three names version 1's Section enum recognizes (mm.ParseSection) -
  // shared with commitMove below, which needs the same test to decide
  // whether "section" means anything for a given destination.
  const backlog = (k) => k === 'ready' || k === 'blocked' || k === 'someday';

  function legality(from, to) {
    // A reorder within the SAME stage is always a plain --move, working
    // included: version 2's working is an ordinary declared stage with its
    // own ordered run (spec-file-format.md §5.1.6), not version 1's
    // separate, order-free slot files, so it has exactly as much order to
    // rearrange as any other stage (spec-gui.md §7.2 - T-0249, removing an
    // earlier client-only restriction that had no library rule behind it).
    // This is distinct from --start on an item already on working, which
    // stays illegal and is never reachable from this branch: to === from
    // here, never the "entering working from elsewhere" case start means.
    if (from === to) return { allowed: true, op: 'move' };
    if (to === 'done') return { allowed: true, op: 'finish' };
    if (from === 'done') return { allowed: false, reason: 'Conflict' };
    // start/pause are legal from or to ANY stage the board declares, not only
    // ready/blocked/someday: the library's startV2/pauseV2 place no
    // restriction on the other side (mm/board_ops.go), so a custom stage
    // (e.g. "review") entering or leaving working is exactly as legal as one
    // of the four default stages doing the same - found live (T-0248): a
    // custom stage was entirely undraggable, in or out, because this table
    // only ever recognized the four literal version-1 names.
    if (to === 'working') return { allowed: true, op: 'start' };
    if (from === 'working') return { allowed: true, op: 'pause' };
    if (backlog(from) && to === 'blocked') return { allowed: true, op: 'block' };
    if (from === 'blocked' && backlog(to)) return { allowed: true, op: 'unblock' };
    // Any other stage-to-stage transition is a plain --move (spec-tools.md
    // §5.1.7 accepts any two declared stages); the four named branches above
    // are only the CLIENT's shortcuts for the sugar ops (start/pause/block/
    // unblock) the library and the item menu both special-case, not an
    // exhaustive list of what is legal. The server is the real authority
    // here (this function's own header comment) - refusing by default would
    // make an entire custom stage undraggable for no reason the library
    // itself enforces.
    //
    // One known gap this does not close: dragging into a stage OTHER than
    // "blocked" that is ALSO listed in needs_reason still refuses server-side
    // ("needs a reason") with no dialog to collect one, because dialog-block
    // (internal/web/templates/partials/dialogs.html) is hardcoded to the
    // literal "blocked" stage, not parameterized by an arbitrary
    // destination. Not reachable on this session's reported board (its only
    // needs_reason stage is the literal "blocked"), so left as a follow-up
    // rather than generalizing the dialog itself here.
    return { allowed: true, op: 'move' };
  }

  /* The move in progress: pointer or keyboard, one shape either way. */
  let move = null;

  const columnKey = (body) => body.getAttribute('data-column');
  const columnBodies = () => Array.from(document.querySelectorAll('.mm-column__body'));
  const cardsIn = (body) => Array.from(body.querySelectorAll('.mm-item'));
  const isCollapsed = (body) => {
    const column = body.closest('.mm-column');
    return Boolean(column) && column.getAttribute('data-collapsed') === 'true';
  };

  /*
    T-0152. Which column body a pointer event lands in.

    A collapsed Someday column (§5.5) hides its body — display:none in mm.css —
    so the pointer hits the header or the rotated title and
    closest('.mm-column__body') resolves to NOTHING, silently discarding the
    drop. Same failure as T-0100 (slack beside a short column), different
    cause: there the body was too short, here it is not in the layout at all.
    So a hit anywhere in a collapsed column counts as a hit on its body — only
    the lookup changes, not what a drop means.
  */
  function bodyAt(target) {
    if (!target || !target.closest) return null;
    const body = target.closest('.mm-column__body');
    if (body) return body;
    const collapsed = target.closest('.mm-column[data-collapsed="true"]');
    return collapsed ? collapsed.querySelector('.mm-column__body') : null;
  }

  function announce(text) {
    const board = document.querySelector('[data-testid="board"]');
    if (board) board.setAttribute('aria-label', text);
  }

  function beginMove(card, keyboard) {
    const body = card.closest('.mm-column__body');
    if (!body) return;

    move = {
      card,
      from: columnKey(body),
      fromBody: body,
      index: cardsIn(body).indexOf(card),
      keyboard: Boolean(keyboard),
      target: body,
      /* The pre-drag DOM, restored verbatim if the server refuses (§7.1). */
      nextSibling: card.nextElementSibling,
    };

    card.setAttribute('data-dragging', 'true');
    root().setAttribute('data-drag-source', `board-column-${move.from}-body`);
    if (keyboard) card.setAttribute('data-move-mode', 'true');

    hoverTarget(body, move.index);
    announce(`Moving ${card.getAttribute('data-item-id')}`);
  }

  /* hoverTarget maintains every attribute §7.3 fixes, on every move. */
  function hoverTarget(body, index) {
    if (!move) return;

    for (const other of columnBodies()) {
      const column = other.closest('.mm-column');
      if (other === body) continue;
      column.removeAttribute('data-drop-target');
      column.removeAttribute('data-drop-allowed');
      column.removeAttribute('data-drop-index');
      column.removeAttribute('data-drop-reason');
    }
    document.querySelectorAll('[data-testid="drop-placeholder"]').forEach((p) => p.remove());

    const column = body.closest('.mm-column');
    const to = columnKey(body);
    const verdict = legality(move.from, to);
    const cards = cardsIn(body).filter((c) => c !== move.card);
    /* Collapsed, there is no visible list to aim within and no card to measure
       against, so the drop lands at the bottom (T-0152) — the one position
       that needs no geometry to mean what it says. */
    const collapsed = isCollapsed(body);
    const clamped = collapsed ? cards.length : Math.max(0, Math.min(index, cards.length));

    move.target = body;
    move.index = clamped;
    move.op = verdict.op;
    move.allowed = verdict.allowed;

    column.setAttribute('data-drop-target', 'true');
    column.setAttribute('data-drop-allowed', verdict.allowed ? 'true' : 'false');
    column.setAttribute('data-drop-index', String(clamped));
    if (verdict.reason) column.setAttribute('data-drop-reason', verdict.reason);
    else column.removeAttribute('data-drop-reason');

    /* The placeholder marks the insertion point (§7.3), absolutely positioned
       out of the flow (T-0149) so it never shifts the card under the pointer
       and loses the release; pointer-events: none (mm.css) keeps events on the
       stable element behind it. */
    const placeholder = document.createElement('div');
    placeholder.setAttribute('data-testid', 'drop-placeholder');
    placeholder.className = 'mm-drop-placeholder';
    placeholder.setAttribute('data-drop-allowed', verdict.allowed ? 'true' : 'false');
    body.appendChild(placeholder);
    /* Inside a hidden body every rect is zero, so positioning it would be
       arithmetic on nothing. The column's own border is the feedback while
       collapsed (mm.css, .mm-column[data-drop-target]). */
    if (collapsed) {
      announce(`${move.card.getAttribute('data-item-id')} to ${to}, position ${clamped + 1}`);
      return;
    }
    const phH = placeholder.offsetHeight;
    const bodyTop = body.getBoundingClientRect().top;
    let center;
    if (clamped === 0) {
      center = bodyTop + phH / 2;
    } else if (clamped >= cards.length) {
      center = cards[cards.length - 1].getBoundingClientRect().bottom + 8;
    } else {
      center = (cards[clamped - 1].getBoundingClientRect().bottom +
                cards[clamped].getBoundingClientRect().top) / 2;
    }
    placeholder.style.top = String(center - phH / 2 - bodyTop) + 'px';

    announce(`${move.card.getAttribute('data-item-id')} to ${to}, position ${clamped + 1}`);
  }

  function endMove() {
    if (!move) return;
    move.card.removeAttribute('data-dragging');
    move.card.removeAttribute('data-move-mode');
    root().removeAttribute('data-drag-source');
    document.querySelectorAll('[data-testid="drop-placeholder"]').forEach((p) => p.remove());
    document.querySelectorAll('.mm-column').forEach((column) => {
      column.removeAttribute('data-drop-target');
      column.removeAttribute('data-drop-allowed');
      column.removeAttribute('data-drop-index');
      column.removeAttribute('data-drop-reason');
    });
    move = null;
  }

  /*
    Commit. An illegal target issues NO REQUEST (§7.2), and a legal one goes
    through htmx.ajax so the response flows through the same swap and
    out-of-band machinery as every other mutation - no parallel update path.
  */
  function commitMove() {
    if (!move) return;
    if (!move.allowed) {
      announce('That move is not allowed');
      endMove();
      return;
    }

    const app = root();
    const project = app.getAttribute('data-project-id');
    const id = move.card.getAttribute('data-item-id');
    const to = columnKey(move.target);
    const op = move.op;
    const index = move.index;
    const card = move.card;

    endMove();

    /* Operations that must prompt before they run (§7.2). Cancelling aborts. */
    if (op === 'block' || op === 'finish' || (op === 'pause' && to === 'blocked')) {
      dialogOpener = card;
      const dialogName = (op === 'pause' && to === 'blocked') ? 'block' : op;
      htmx.ajax('GET', `/p/${project}/dialog/${dialogName}?item=${id}`, {
        target: "[data-testid='dialog-root']",
      });
      return;
    }

    const values = { position: String(index + 1) };
    if (op === 'move' || op === 'pause') {
      /* Which column it landed in. A pause names it because §7.2 takes the
         position from the drop index; a move within the backlog names it
         because the section may have changed. stage (version 2) is sent
         unconditionally - the server dispatches on the directory's actual
         version and reads only the field that applies (internal/web/
         item.go's own "move"/"pause" cases), the same both-fields
         convention its own block/unblock handlers already use. Omitting
         stage silently no-ops a version-2 move: with no stage field,
         moveV2/pauseV2 default the destination to the item's CURRENT stage,
         so the drop reduces to a same-stage reposition - the "dragged into
         Someday, it stayed in Ready" bug T-0243 fixed.

         section (version 1) is sent ONLY when to is one of the three names
         mm.ParseSection actually recognizes - unlike stage, section is not
         a free-form slug, and item.go's handler refuses the request
         outright on an unrecognized one before it ever reaches the stage
         field. Sending it unconditionally alongside stage broke every drag
         into or within a custom version-2 stage (e.g. "review") with a
         flat 400 "not a section", regardless of what legality() decided -
         found live (T-0248), the same day legality() was generalized past
         the four version-1 names. */
      values.stage = to;
      if (backlog(to)) values.section = to;
    }

    htmx.ajax('POST', `/p/${project}/items/${id}/${op}`, {
      target: "[data-testid='board']",
      /* morph, not outerHTML (T-0161): same reasoning as the card-menu op
         above — the T-0138 poll-chain hazard is gone (no `every` clause
         survives in any template), so a diffed board preserves the drop
         target's focus and hover instead of rebuilding everything. */
      swap: 'morph',
      // See the item-action htmx.ajax call above: source is required for the
      // morph extension to resolve for this request at all.
      source: app,
      values,
    }).catch(() => {
      /* A rejected drop restores the pre-drag DOM: the board is re-rendered
         from the server on success, and left untouched on failure, so nothing
         optimistic has to be unwound. §7.1 permits an optimistic update only
         when it can be fully reverted - not doing one is the simpler way to
         satisfy that. */
      announce('The move was refused');
    });
  }

  /* ------------------------------------------------------- pointer drags */

  document.addEventListener('dragstart', (event) => {
    const card = event.target.closest('.mm-item');
    if (!card) return;
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('text/plain', card.getAttribute('data-item-id'));
    beginMove(card, false);
  });

  /* The insertion index is where the pointer is, not where the card is. Cards
     are measured at natural positions: the placeholder is out of flow (T-0149)
     and displaces nothing, so the T-0110 feedback loop cannot recur. */
  function indexAt(body, clientY) {
    const cards = cardsIn(body).filter((c) => c !== move.card);
    for (let i = 0; i < cards.length; i += 1) {
      const box = cards[i].getBoundingClientRect();
      if (clientY < box.top + box.height / 2) return i;
    }
    return cards.length;
  }

  document.addEventListener('dragover', (event) => {
    if (!move) return;
    const body = bodyAt(event.target);
    if (!body) return;
    event.preventDefault();
    hoverTarget(body, indexAt(body, event.clientY));
    event.dataTransfer.dropEffect = move.allowed ? 'move' : 'none';
  });

  document.addEventListener('drop', (event) => {
    if (!move) return;
    event.preventDefault();

    /* The drop's own coordinates are authoritative: a release within a frame
       of a fast final move can leave move.index one dragover stale (T-0149). */
    const body = bodyAt(event.target);
    if (body) hoverTarget(body, indexAt(body, event.clientY));
    commitMove();
  });

  document.addEventListener('dragend', () => endMove());

  /* ------------------------------------------------------ keyboard moves */

  /*
    §7.4, required and normative rather than an accessibility afterthought.
    Space or Enter enters move mode, arrows change index and column, Enter
    commits, Escape cancels and restores focus and position.
  */
  document.addEventListener('keydown', (event) => {
    const card = event.target.closest && event.target.closest('.mm-item');

    if (!move) {
      if (!card) return;
      if (event.key === ' ' || event.key === 'Enter') {
        event.preventDefault();
        beginMove(card, true);
      }
      return;
    }

    const bodies = columnBodies();
    const current = bodies.indexOf(move.target);

    switch (event.key) {
      case 'ArrowUp':
        event.preventDefault();
        hoverTarget(move.target, move.index - 1);
        break;
      case 'ArrowDown':
        event.preventDefault();
        hoverTarget(move.target, move.index + 1);
        break;
      case 'ArrowLeft':
        event.preventDefault();
        hoverTarget(bodies[Math.max(0, current - 1)], move.index);
        break;
      case 'ArrowRight':
        event.preventDefault();
        hoverTarget(bodies[Math.min(bodies.length - 1, current + 1)], move.index);
        break;
      case 'Enter':
        event.preventDefault();
        commitMove();
        break;
      case 'Escape': {
        event.preventDefault();
        const focus = move.card;
        endMove();
        focus.focus();
        break;
      }
      default:
        break;
    }
  });

  /* Collapsible columns (§5.5, generalized in T-0240 from a single Someday
     flag): a client preference in localStorage, sent with every htmx request
     as a comma-separated list (T-0150) so a refresh is born in the right
     state. */
  function collapsedStorageKey() {
    const project = root()?.getAttribute('data-project-id');
    return project ? `mm:collapsed-stages:${project}` : null;
  }

  function collapsedStageSet() {
    try {
      const key = collapsedStorageKey();
      if (!key) return new Set();
      let stored = localStorage.getItem(key);
      if (stored === null) {
        /* No preference recorded for this project yet - not the same as
           having recorded "none collapsed" - so seed from the server's
           default (ui.board.collapsedStages), carried on the board root. */
        const board = document.querySelector('[data-testid="board"]');
        stored = board?.getAttribute('data-default-collapsed-stages') || '';
      }
      return new Set(stored.split(',').map((s) => s.trim()).filter(Boolean));
    } catch (_) { return new Set(); }
  }

  function saveCollapsedStageSet(set) {
    try {
      const key = collapsedStorageKey();
      if (key) localStorage.setItem(key, Array.from(set).join(','));
    } catch (_) {}
  }

  document.body.addEventListener('htmx:configRequest', (event) => {
    const headers = event.detail && event.detail.headers;
    if (headers) headers['X-Collapsed-Stages'] = Array.from(collapsedStageSet()).join(',');
  });

  /* A column's key is read back out of its own testid (board-column-<key>,
     board-column-<key>-toggle) rather than a new data-* attribute, since the
     testid already carries it exactly (board.go's Testid field). */
  function columnKeyFromTestid(testid, suffix) {
    if (!testid || !testid.startsWith('board-column-')) return null;
    let key = testid.slice('board-column-'.length);
    if (suffix && key.endsWith(suffix)) key = key.slice(0, -suffix.length);
    return key || null;
  }

  function restoreCollapsedState() {
    const set = collapsedStageSet();
    if (set.size === 0) return;
    document.querySelectorAll('.mm-column').forEach((col) => {
      const key = columnKeyFromTestid(col.getAttribute('data-testid'), '');
      if (!key || !set.has(key)) return;
      col.setAttribute('data-collapsed', 'true');
      const toggle = col.querySelector('.mm-column__toggle');
      if (toggle) toggle.textContent = 'v';
    });
  }

  document.body.addEventListener('click', (e) => {
    const toggle = e.target.closest('.mm-column__toggle');
    if (!toggle) return;
    const key = columnKeyFromTestid(toggle.getAttribute('data-testid'), '-toggle');
    const col = toggle.closest('.mm-column');
    if (!key || !col) return;
    const isCollapsed = col.getAttribute('data-collapsed') === 'true';
    const nextState = !isCollapsed;
    col.setAttribute('data-collapsed', nextState ? 'true' : 'false');
    toggle.textContent = nextState ? 'v' : '>';
    const set = collapsedStageSet();
    if (nextState) set.add(key);
    else set.delete(key);
    saveCollapsedStageSet(set);
  });

  /* afterSwap, not afterSettle - before paint (T-0150). */
  document.body.addEventListener('htmx:afterSwap', restoreCollapsedState);
  document.addEventListener('DOMContentLoaded', restoreCollapsedState);

  /* -------------------------------------------- resizable item panel (T-0242) */

  /* A client preference in localStorage, persisted as a percentage of the
     viewport width so it survives a resized browser window sanely - a raw
     pixel width would not. 33 is both the default and the floor: the panel
     never starts, and can never be dragged, narrower than a third of the
     viewport. 90 is the ceiling, leaving a sliver of the board in view. */
  const PANEL_MIN_VW = 33;
  const PANEL_MAX_VW = 90;

  function panelWidthVw() {
    try {
      const stored = parseFloat(localStorage.getItem('mm:item-panel-width-vw'));
      if (!Number.isFinite(stored)) return PANEL_MIN_VW;
      return Math.min(PANEL_MAX_VW, Math.max(PANEL_MIN_VW, stored));
    } catch (_) { return PANEL_MIN_VW; }
  }

  function saveWidthVw(vw) {
    try { localStorage.setItem('mm:item-panel-width-vw', String(vw)); } catch (_) {}
  }

  /* Applied on every render the panel can appear in - a fresh /item/:id load,
     the new-item panel, and "save and add another" reopening a copy over the
     board (T-0080) - so it is never born at the CSS default and then jumped
     to the stored width a frame later. */
  function applyPanelWidth() {
    const panel = document.querySelector('[data-testid="item-panel"]');
    if (panel) panel.style.width = panelWidthVw() + 'vw';
  }

  document.body.addEventListener('htmx:afterSwap', applyPanelWidth);
  document.addEventListener('DOMContentLoaded', applyPanelWidth);

  document.body.addEventListener('pointerdown', (e) => {
    const handle = e.target.closest('[data-testid="x-item-panel-resize-handle"]');
    if (!handle) return;
    const panel = handle.closest('[data-testid="item-panel"]');
    if (!panel) return;
    e.preventDefault();
    try { handle.setPointerCapture(e.pointerId); } catch (_) { /* not in jsdom */ }
    handle.setAttribute('data-dragging', 'true');

    const clamp = (vw) => Math.min(PANEL_MAX_VW, Math.max(PANEL_MIN_VW, vw));
    /* The panel is anchored right:0, so its width is the distance from the
       pointer to the RIGHT edge of the viewport, not the left. */
    const widthAt = (clientX) => clamp(((window.innerWidth - clientX) / window.innerWidth) * 100);

    const move = (ev) => { panel.style.width = widthAt(ev.clientX) + 'vw'; };
    const up = (ev) => {
      handle.removeAttribute('data-dragging');
      saveWidthVw(widthAt(ev.clientX));
      document.removeEventListener('pointermove', move);
      document.removeEventListener('pointerup', up);
    };
    document.addEventListener('pointermove', move);
    document.addEventListener('pointerup', up);
  });

  /* --------------------------------------------------- SSE-down polling */

  /*
    T-0139 (D2). The freshness backstop used to be an unconditional `every 30s`
    clause on every region's trigger. The T-0131 measurement showed that on an
    idle project with a healthy stream that is 100% of the refresh traffic and
    100% waste — 65 fetches, zero SSE events, 20/20 byte-identical. The
    backstop only needs to exist while the stream is DOWN, so the triggers'
    `every 30s` clause is gone and this interval takes over on sseClose /
    sseError, stopping again on sseOpen (the extension fires sseOpen on
    reconnect, and on the shell swap that follows a re-theme).

    The regions self-describe: each carries its own hx-get and an hx-trigger
    naming its sse: event, and swaps morph — the marker of a region that
    refreshes itself from a fragment. The app root's outerHTML theme shell is
    deliberately NOT polled: a full app re-render every 5 s is exactly the
    waste this exists to avoid. The morph extension resolves because each
    region declares hx-ext itself (TestMorphSwapsDeclareOwnExtension).
  */
  const POLL_INTERVAL_MS = 5000;
  let pollTimer = null;

  function sseRegions() {
    const found = document.querySelectorAll('[hx-trigger*="sse:"], [data-hx-trigger*="sse:"]');
    return Array.from(found).filter((el) =>
      (el.getAttribute('hx-swap') || el.getAttribute('data-hx-swap')) === 'morph');
  }

  function pollRegion(el) {
    const url = el.getAttribute('hx-get') || el.getAttribute('data-hx-get');
    if (!url) return;
    htmx.ajax('GET', url, { target: el, swap: 'morph', source: el });
  }

  function startPolling() {
    if (pollTimer) return;
    pollTimer = setInterval(() => {
      sseRegions().forEach(pollRegion);
    }, POLL_INTERVAL_MS);
    // Converge immediately: spec-gui.md §2.3's polling-alone guarantee must
    // not wait half a window.
    sseRegions().forEach(pollRegion);
  }

  function stopPolling() {
    if (pollTimer) {
      clearInterval(pollTimer);
      pollTimer = null;
    }
  }

  document.body.addEventListener('htmx:sseClose', startPolling);
  document.body.addEventListener('htmx:sseError', startPolling);
  document.body.addEventListener('htmx:sseOpen', stopPolling);

  setBusy();
})();
