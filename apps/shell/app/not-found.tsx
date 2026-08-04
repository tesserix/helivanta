export default function NotFound() {
  return (
    <main className="flex min-h-screen flex-col items-center justify-center gap-2 p-6 text-center">
      <h1 className="text-xl font-semibold text-foreground">Page not found</h1>
      <p className="text-sm text-muted-foreground">
        Check the address, or head back to the{" "}
        <a className="underline underline-offset-4" href="/">
          dashboard
        </a>
        .
      </p>
    </main>
  );
}
