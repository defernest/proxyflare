/**
 * FlareProx - Cloudflare Worker URL Redirection Script
 * TypeScript implementation optimized for cold start & execution time
 */

export interface Env {
    AUTH_KEY: string;
    CORS_ORIGIN?: string;
    CORS_METHODS?: string;
    CORS_ALLOWED_HEADERS?: string;
}

export default {
    async fetch(request: Request, env: Env, ctx: ExecutionContext): Promise<Response> {
        // get target URL from header or query parameter
        const urlInParams = new URL(request.url).searchParams.get("url");
        const targetStr = request.headers.get("X-Target-URL") || urlInParams;
        if (!targetStr) {
            return new Response("Target URL not found", { status: 400 });
        }

        // try to parse target URL
        let targetUrl: URL;
        try {
            targetUrl = new URL(targetStr);
        } catch (error) {
            return new Response("Invalid target URL", { status: 400 });
        }

        // search and delete query params
        const searchParams = new URLSearchParams(targetUrl.search);
        searchParams.delete('_cb');
        searchParams.delete('_t');
        searchParams.delete("url");
        targetUrl.search = searchParams.toString();

        // work with headers
        const headers = new Headers(request.headers);
        headers.delete("X-Target-URL");
        headers.delete("Authorization");
        headers.set("Host", targetUrl.hostname);
        const clientIp = request.headers.get("CF-Connecting-IP") || "127.0.0.1";
        headers.set("X-Forwarded-For", clientIp);
        // forward request to target URL
        try {
            const response = await fetch(new Request(targetUrl, {
                method: request.method,
                headers: headers,
                body: request.body,
                redirect: "manual"
            }));

            // cleanup encoding headers
            const responseHeaders = new Headers(response.headers);
            responseHeaders.delete("content-encoding");
            responseHeaders.delete("content-length");
            responseHeaders.delete("transfer-encoding");

            // add cors headers
            responseHeaders.set("Access-Control-Allow-Origin", env.CORS_ORIGIN || "*");
            responseHeaders.set("Access-Control-Allow-Methods", env.CORS_METHODS || "GET, POST, PUT, DELETE, OPTIONS, PATCH, HEAD");
            responseHeaders.set("Access-Control-Allow-Headers", env.CORS_ALLOWED_HEADERS || "*");

            // if this is a preflight request, return empty response
            if (request.method === "OPTIONS") {
                return new Response(null, {
                    status: 204,
                    headers: responseHeaders
                });
            }
            return new Response(response.body, {
                status: response.status,
                statusText: response.statusText,
                headers: responseHeaders
            });
        } catch (error) {
            return new Response("Failed to fetch target URL", { status: 500 });
        }
    }
}
