// Example: TipTap ProseMirror extension that pushes ProofAPI matches
// into the editor as inline decorations (wavy underlines).
//
// Install: npm install @tiptap/core @tiptap/pm

import { Extension } from "@tiptap/core";
import { Plugin, PluginKey } from "@tiptap/pm/state";
import { Decoration, DecorationSet } from "@tiptap/pm/view";
import type { Match } from "../types/proof";

export const SPELL_PLUGIN_KEY = new PluginKey("spellcheck");

const CLASS: Record<string, string> = {
  misspelling:   "sp-spell",
  grammar:       "sp-grammar",
  style:         "sp-style",
  typographical: "sp-punct",
};

export interface SpellCheckOptions {
  onTextChange: (text: string) => void;
}

export const SpellCheckExtension = Extension.create<SpellCheckOptions>({
  name: "spellcheck",

  addOptions: () => ({ onTextChange: () => {} }),

  addProseMirrorPlugins() {
    const { onTextChange } = this.options;

    return [
      new Plugin({
        key: SPELL_PLUGIN_KEY,

        state: {
          init: () => DecorationSet.empty,
          apply(tr, old) {
            const meta = tr.getMeta(SPELL_PLUGIN_KEY);
            if (meta?.matches !== undefined) {
              return DecorationSet.create(
                tr.doc,
                (meta.matches as Match[]).map((m) =>
                  Decoration.inline(m.offset, m.offset + m.length, {
                    class: CLASS[m.rule.issueType] ?? "sp-grammar",
                    "data-match": JSON.stringify(m),
                  })
                )
              );
            }
            return tr.docChanged ? old.map(tr.mapping, tr.doc) : old;
          },
        },

        props: {
          decorations: (state) =>
            SPELL_PLUGIN_KEY.getState(state) ?? DecorationSet.empty,
        },

        view: () => ({
          update(view, prev) {
            if (!view.state.doc.eq(prev.doc)) {
              onTextChange(view.state.doc.textContent);
            }
          },
        }),
      }),
    ];
  },
});

export function applyMatches(editor: any, matches: Match[]) {
  if (!editor?.view) return;
  const { state, dispatch } = editor.view;
  dispatch(state.tr.setMeta(SPELL_PLUGIN_KEY, { matches }));
}
