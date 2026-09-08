import {EditorState} from '@codemirror/state';
import {
  EditorView,
  drawSelection,
  highlightSpecialChars,
  keymap,
  rectangularSelection,
} from '@codemirror/view';
import {
  defaultKeymap,
  history,
  historyKeymap,
} from '@codemirror/commands';
import {bracketMatching, HighlightStyle, syntaxHighlighting} from '@codemirror/language';
import {markdown} from '@codemirror/lang-markdown';
import {searchKeymap} from '@codemirror/search';
import {tags} from '@lezer/highlight';

const markdownHighlightStyle = HighlightStyle.define([
  {tag: tags.heading, color: 'var(--mm-color-accent-base)', fontWeight: 'bold'},
  {tag: [tags.link, tags.url], color: 'var(--mm-color-link)', textDecoration: 'underline'},
  {tag: [tags.emphasis], fontStyle: 'italic'},
  {tag: [tags.strong], fontWeight: 'bold'},
  {tag: [tags.monospace, tags.processingInstruction], color: 'var(--mm-color-feedback-info)'},
  {tag: [tags.quote, tags.comment], color: 'var(--mm-color-fg-muted)'},
  {tag: [tags.meta, tags.contentSeparator], color: 'var(--mm-color-fg-subtle)'},
]);

const extensions = [
  highlightSpecialChars(),
  history(),
  drawSelection(),
  rectangularSelection(),
  EditorView.lineWrapping,
  bracketMatching(),
  syntaxHighlighting(markdownHighlightStyle),
  markdown(),
  keymap.of([...defaultKeymap, ...historyKeymap, ...searchKeymap]),
];

function create(textarea) {
  if (!textarea || textarea._mmCodeMirror) return textarea && textarea._mmCodeMirror;
  const mount = document.createElement('div');
  mount.className = 'mm-codemirror';
  mount.setAttribute('data-testid', 'x-item-detail-codemirror');
  textarea.insertAdjacentElement('beforebegin', mount);

  let view;
  try {
    const state = EditorState.create({
      doc: textarea.value,
      extensions: extensions.concat(EditorView.contentAttributes.of({
        'aria-label': textarea.getAttribute('aria-label') || 'Detail',
        'aria-labelledby': textarea.getAttribute('aria-labelledby') || undefined,
        'aria-describedby': textarea.getAttribute('aria-describedby') || undefined,
        'aria-multiline': 'true',
        spellcheck: 'true',
      }), EditorView.updateListener.of((update) => {
        if (!update.docChanged) return;
        textarea.value = update.state.doc.toString();
        textarea.dispatchEvent(new Event('input', {bubbles: true}));
      })),
    });
    view = new EditorView({state, parent: mount});
  } catch (error) {
    mount.remove();
    throw error;
  }

  textarea.hidden = true;
  const editor = {
    view,
    capture() {
      const selection = view.state.selection.main;
      return {anchor: selection.anchor, head: selection.head, scrollTop: view.scrollDOM.scrollTop};
    },
    restore(saved) {
      if (!saved) return;
      const length = view.state.doc.length;
      view.dispatch({selection: {
        anchor: Math.min(saved.anchor, length),
        head: Math.min(saved.head, length),
      }});
      view.scrollDOM.scrollTop = saved.scrollTop || 0;
    },
    focus() { view.focus(); },
    destroy() {
      view.destroy();
      mount.remove();
      textarea.hidden = false;
      delete textarea._mmCodeMirror;
    },
  };
  textarea._mmCodeMirror = editor;
  return editor;
}

window.mmCodeMirror = Object.freeze({create});
