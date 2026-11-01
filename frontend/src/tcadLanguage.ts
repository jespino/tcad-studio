// Syntax highlighting for TCAD (a StreamLanguage: tokens only, no grammar).
// The keywords are those of internal/dsl/parser.go.
import { StreamLanguage } from '@codemirror/language';
import { tags as t } from '@lezer/highlight';

const KEYWORDS = /^(let|param|module|func|for|in|if|else|echo)\b/;
const OPERATIONS = /^(hole|solid|group|intersect)\b/;
const ATOMS = /^(true|false)\b/;

export const tcadLanguage = StreamLanguage.define<{ afterDecl: boolean }>({
  name: 'tcad',
  startState: () => ({ afterDecl: false }),
  token(stream, state) {
    if (stream.sol()) state.afterDecl = false;
    if (stream.eatSpace()) return null;
    if (stream.match('//')) {
      stream.skipToEnd();
      return 'comment';
    }
    if (stream.match('/*')) {
      while (!stream.eol() && !stream.match('*/')) stream.next();
      return 'comment';
    }
    if (stream.match(/^"(?:[^"\\]|\\.)*"?/)) return 'string';
    if (stream.match(/^-?(?:\d+\.?\d*|\.\d+)(?:e[+-]?\d+)?/i)) return 'number';
    if (stream.match(/^(let|param|module|func)\b/)) {
      state.afterDecl = true;
      return 'keyword';
    }
    if (stream.match(KEYWORDS)) return 'keyword';
    if (stream.match(OPERATIONS)) return 'atom';
    if (stream.match(ATOMS)) return 'atom';
    if (state.afterDecl && stream.match(/^\$?[A-Za-z_]\w*/)) {
      state.afterDecl = false;
      return 'definition';
    }
    // a call: box(…), .move(…)
    if (stream.match(/^[A-Za-z_]\w*(?=\s*\()/)) return 'function';
    if (stream.match(/^[A-Za-z_]\w*(?=\s*=[^=])/)) return 'propertyName';
    if (stream.match(/^\$?[A-Za-z_]\w*/)) return 'variableName';
    stream.next();
    return null;
  },
  tokenTable: { definition: t.definition(t.variableName), function: t.function(t.variableName) },
  languageData: { commentTokens: { line: '//' } },
});
