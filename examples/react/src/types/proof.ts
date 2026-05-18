// Shared TypeScript types for ProofAPI responses.

export interface Match {
  message: string;
  offset: number;
  length: number;
  replacements: { value: string }[];
  rule: {
    id: string;
    issueType: "misspelling" | "grammar" | "style" | "typographical";
    category: { id: string; name: string };
  };
  context: { text: string; offset: number; length: number };
}

export interface SpellCheckResult {
  matches: Match[];
  cached: boolean;
  latencyMs: number;
}
