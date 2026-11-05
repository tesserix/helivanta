export function formatPostingUtc(value: string) {
  const utc = new Date(value).toISOString();
  const microseconds = value.match(/\.(\d{3})(\d{1,3})/)?.[2];
  return microseconds ? `${utc.slice(0, -1)}${microseconds.padEnd(3, "0")}Z` : utc;
}
