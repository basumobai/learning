import type { NextConfig } from "next";

const nextConfig: NextConfig = {
  async redirects() {
    return [
      { source: "/admin/resource", destination: "/admin/resources", permanent: false },
    ];
  },
  async headers() {
    return [
      {
        source: "/admin/:path*",
        headers: [
          { key: "Cache-Control", value: "private, no-store, max-age=0, must-revalidate" },
        ],
      },
      {
        source: "/:path*",
        headers: [
          { key: "X-Content-Type-Options", value: "nosniff" },
          { key: "Referrer-Policy", value: "same-origin" },
          {
            key: "Content-Security-Policy",
            value:
              "frame-ancestors 'none'; object-src 'none'; base-uri 'self'; form-action 'self'",
          },
          {
            key: "Permissions-Policy",
            value: "camera=(), microphone=(self), geolocation=()",
          },
        ],
      },
      {
        source: "/api/v1/admin/resources/:id/file",
        headers: [{key:"Content-Security-Policy",value:"frame-ancestors 'self'; object-src 'none'; base-uri 'self'"}],
      },
    ];
  },
};

export default nextConfig;
