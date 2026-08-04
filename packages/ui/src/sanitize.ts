import DOMPurify from "dompurify";

// The only allowed path to dangerouslySetInnerHTML (lint-enforced).
export function sanitizeHtml(html: string): { __html: string } {
  return { __html: DOMPurify.sanitize(html) };
}
