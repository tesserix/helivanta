import type { ReactNode } from "react";

/**
 * The one heading every workspace page opens with (#1255 spec D3). The shell
 * header owns the page's h1, so the title is an h2. Deliberately plain: no
 * eyebrow, accent line or slogan styling — a working tool, not a landing page.
 */
export function PageHeading({
  title,
  description,
  actions,
}: {
  title: string;
  description?: ReactNode;
  actions?: ReactNode;
}) {
  return (
    <header className="mb-6 flex flex-wrap items-start justify-between gap-4">
      <div className="min-w-0">
        <h2 className="text-xl font-semibold text-foreground">{title}</h2>
        {description && <p className="mt-1 text-sm text-muted-foreground">{description}</p>}
      </div>
      {actions && <div className="flex flex-wrap items-center gap-2">{actions}</div>}
    </header>
  );
}
