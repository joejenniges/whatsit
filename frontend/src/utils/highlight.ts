/**
 * Split text into segments based on regex matches.
 * Returns an array of { text, matched } objects for rendering
 * with highlighted matches.
 */
export interface TextSegment {
  text: string;
  matched: boolean;
}

export function splitAtMatches(text: string, regex: RegExp | null): TextSegment[] {
  if (!regex || !text) {
    return [{ text: text || '', matched: false }];
  }

  const segments: TextSegment[] = [];
  // Clone regex with global flag to use exec iteratively
  const global = new RegExp(regex.source, regex.flags.includes('g') ? regex.flags : regex.flags + 'g');
  let lastIndex = 0;
  let match: RegExpExecArray | null;

  while ((match = global.exec(text)) !== null) {
    if (match.index > lastIndex) {
      segments.push({ text: text.slice(lastIndex, match.index), matched: false });
    }
    segments.push({ text: match[0], matched: true });
    lastIndex = global.lastIndex;
    // Prevent infinite loop on zero-length match
    if (match[0].length === 0) {
      global.lastIndex++;
    }
  }

  if (lastIndex < text.length) {
    segments.push({ text: text.slice(lastIndex), matched: false });
  }

  if (segments.length === 0) {
    return [{ text, matched: false }];
  }

  return segments;
}
