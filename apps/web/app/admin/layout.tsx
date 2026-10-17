// Administration contains frequently changing authenticated settings.
// Avoid serving a static HTML snapshot after a Docker deployment.
export const dynamic = "force-dynamic";
export const revalidate = 0;

export default function AdminLayout({ children }: { children: React.ReactNode }) {
  return children;
}
