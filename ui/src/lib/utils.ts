export function cn(...classes: Array<string | false | null | undefined>) {
  return classes.filter(Boolean).join(' ')
}

export function parseNumber(s: string | undefined): number | null {
  if (s == null) return null
  const n = Number(s)
  return Number.isFinite(n) ? n : null
}

/**
 * Formats a number with thousand separators based on browser locale
 * @param value - The number to format
 * @param decimals - Number of decimal places (default: 0)
 * @returns Formatted number string with thousand separators
 */
export function formatNumber(value: number, decimals: number = 0): string {
  return new Intl.NumberFormat(undefined, {
    minimumFractionDigits: decimals,
    maximumFractionDigits: decimals,
  }).format(value)
}
