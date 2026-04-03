// Splash language grammar for Prism.js

Prism.languages.splash = {

  // /// doc comments — must come before plain // comments
  'doc-comment': {
    pattern: /\/\/\/.*/,
    greedy: true,
    alias: 'comment'
  },

  'comment': {
    pattern: /\/\/.*/,
    greedy: true
  },

  'string': {
    pattern: /"(?:[^"\\]|\\.)*"/,
    greedy: true
  },

  // Annotations — the most visually distinctive Splash feature
  'annotation': {
    pattern: /@\w+/
  },

  // Effects — appear after `needs` keyword
  'effect': {
    pattern: /\b(?:DB\.(?:read|write|admin)|DB|Net|AI|Agent|FS|Exec|Cache|Secrets(?:\.(?:read|write))?|Queue|Metric|Store|Clock)\b/
  },

  'keyword': {
    pattern: /\b(?:fn|type|enum|let|return|if|else|needs|module|use|expose|constraint|match|group|catch|migration|up|down|policy)\b/
  },

  // Built-in types
  'builtin': {
    pattern: /\b(?:Int|Float|String|Bool|List|Result|Stream|Embedding|Loggable|Serializable)\b/
  },

  'boolean': {
    pattern: /\b(?:true|false|none)\b/
  },

  'number': {
    pattern: /\b\d+(?:\.\d+)?\b/
  },

  'operator': /->|=>|\?\?|[+\-*\/=<>!|&]+/,

  'punctuation': /[{}[\](),.;:<>]/
};

// Shell sessions — reuse for `$ splash check ...` blocks
Prism.languages.shell = Prism.languages.extend('bash', {});
