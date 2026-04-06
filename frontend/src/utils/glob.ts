/**
 * Convert a glob pattern (using * as wildcard) to a case-insensitive regex.
 * Only * is special -- everything else is literal (regex metacharacters escaped).
 */
export function globToRegex(pattern: string): RegExp | null {
  if (!pattern.trim()) return null;

  const parts = pattern.split('*');
  const escaped = parts.map(p =>
    p.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
  );
  const regexStr = escaped.join('.*');

  try {
    return new RegExp(regexStr, 'i');
  } catch {
    return null;
  }
}
