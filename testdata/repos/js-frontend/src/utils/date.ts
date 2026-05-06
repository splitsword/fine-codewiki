export function formatDate(date: Date): string {
  return date.toLocaleDateString('zh-CN');
}

export function parseDate(input: string): Date {
  return new Date(input);
}
