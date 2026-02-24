use url::Url;
use worker::*;

mod utils;

/// Params to filter from the proxied URL (cache-busters and routing param).
const FILTERED_PARAMS: &[&str] = &["url", "_cb", "_t"];

fn generate_random_ip() -> String {
    let now = Date::now().as_millis();
    let mut seed = now;

    // Simple Linear Congruential Generator (LCG)
    // Using constants from MMIX by Donald Knuth
    let mut next_rand = || {
        seed = seed
            .wrapping_mul(6364136223846793005)
            .wrapping_add(1442695040888963407);
        // Extract 8 bits from the high-order bits
        ((seed >> 32) & 0xFF) as u8
    };

    // Ensure first octet is not 0 (though 0 is technically valid IP, often reserved)
    // Python implementation uses 1-255.
    let o1 = match next_rand() {
        0 => 1,
        x => x,
    };

    format!("{}.{}.{}.{}", o1, next_rand(), next_rand(), next_rand())
}

fn log_request(req: &Request) {
    let (coords, region, country) = if let Some(cf) = req.cf() {
        (
            cf.coordinates().unwrap_or_default(),
            cf.region().unwrap_or_else(|| "unknown region".into()),
            cf.country().unwrap_or_else(|| "unknown country".into()),
        )
    } else {
        (
            (0.0, 0.0),
            "unknown region".into(),
            "unknown country".into(),
        )
    };

    console_log!(
        "{} - [{:?}], located at: {:?}, within: {}",
        req.path(),
        coords,
        region,
        country
    );
}

#[event(fetch)]
pub async fn main(req: Request, env: Env, _ctx: worker::Context) -> Result<Response> {
    match do_main(req, env).await {
        Ok(resp) => Ok(resp),
        Err(e) => {
            console_log!("CRITICAL ERROR: {:?}", e);
            Response::error(format!("Debug Error: {:?}", e), 500)
        }
    }
}

pub async fn do_main(req: Request, _env: Env) -> Result<Response> {
    log_request(&req);
    utils::set_panic_hook();

    let method = req.method();

    // 0. Handle CORS preflight
    if method == Method::Options {
        let headers = Headers::new();
        headers.set("Access-Control-Allow-Origin", "*")?;
        headers.set(
            "Access-Control-Allow-Methods",
            "GET, POST, PUT, DELETE, OPTIONS, PATCH, HEAD",
        )?;
        headers.set("Access-Control-Allow-Headers", "*")?;

        return Ok(Response::empty()?.with_status(204).with_headers(headers));
    }

    // 1. Parse the target URL
    let url = req.url()?;
    let query_pairs = url.query_pairs();
    let mut target_url_str: Option<String> = None;

    // 1.1 Query Param
    for (key, value) in query_pairs {
        if key == "url" {
            target_url_str = Some(value.into_owned());
            break;
        }
    }

    // 1.2 Header
    if target_url_str.is_none() {
        if let Ok(Some(header_val)) = req.headers().get("X-Target-URL") {
            target_url_str = Some(header_val);
        }
    }

    // 1.3 Path (e.g. /https://example.com)
    if target_url_str.is_none() && url.path() != "/" {
        let path = url.path().trim_start_matches('/');
        if path.starts_with("http") {
            target_url_str = Some(path.to_string());
        }
    }

    let target_url_val = match target_url_str {
        Some(u) => u,
        None => {
            // Return early to avoid unused references if we were to proceed
            return Response::error("Missing target URL", 400);
        }
    };

    // Validate URL
    let mut target_url = match Url::parse(&target_url_val) {
        Ok(u) => u,
        Err(_) => return Response::error("Invalid target URL", 400),
    };

    // Filter out cache-buster and routing query params
    // Collect extra params from the worker URL that aren't filtered
    let extra_params: Vec<(String, String)> = url
        .query_pairs()
        .filter(|(k, _)| !FILTERED_PARAMS.contains(&k.as_ref()))
        .map(|(k, v)| (k.into_owned(), v.into_owned()))
        .collect();

    // Rebuild target URL query: keep target's own params + add extra non-filtered params
    {
        let existing: Vec<(String, String)> = target_url
            .query_pairs()
            .filter(|(k, _)| !FILTERED_PARAMS.contains(&k.as_ref()))
            .map(|(k, v)| (k.into_owned(), v.into_owned()))
            .collect();
        if existing.is_empty() && extra_params.is_empty() {
            target_url.set_query(None);
        } else {
            let mut pairs = target_url.query_pairs_mut();
            pairs.clear();
            for (k, v) in &existing {
                pairs.append_pair(k, v);
            }
            for (k, v) in &extra_params {
                pairs.append_pair(k, v);
            }
        }
    }

    // 2. Prepare headers
    let headers = Headers::new();
    let mut has_forwarded_for = false;
    for (key, value) in req.headers() {
        let key_lower = key.to_lowercase();
        match key_lower.as_str() {
            "host" | "cf-connecting-ip" | "cf-ipcountry" | "cf-ray" | "cf-visitor" => continue,
            "x-my-x-forwarded-for" => {
                headers.set("X-Forwarded-For", &value)?;
                has_forwarded_for = true;
            }
            _ => {
                headers.set(&key, &value)?;
            }
        }
    }
    if !has_forwarded_for {
        headers.set("X-Forwarded-For", &generate_random_ip())?;
    }

    // 3. Request Body & Init
    let mut init = RequestInit::new();
    init.with_method(method.clone());
    init.with_headers(headers);

    if method != Method::Get && method != Method::Head {
        // req.inner() returns &web_sys::Request.
        // req.inner().body() returns Option<ReadableStream>.
        // ReadableStream implements Into<JsValue>.
        if let Some(body_stream) = req.inner().body() {
            init.with_body(Some(body_stream.into()));
        }
    }

    // 4. Fetch
    let fetch_request = Request::new_with_init(target_url.as_str(), &init)?;
    let mut response = Fetch::Request(fetch_request).send().await?;

    // 5. Process Response Headers
    let new_headers = Headers::new();
    for (key, value) in response.headers() {
        let key_lower = key.to_lowercase();
        if !matches!(
            key_lower.as_str(),
            "content-encoding" | "content-length" | "transfer-encoding"
        ) {
            new_headers.set(&key, &value)?;
        }
    }

    // Add CORS
    new_headers.set("Access-Control-Allow-Origin", "*")?;
    new_headers.set(
        "Access-Control-Allow-Methods",
        "GET, POST, PUT, DELETE, OPTIONS, PATCH, HEAD",
    )?;
    new_headers.set("Access-Control-Allow-Headers", "*")?;

    // 6. Return Response
    // We use Response::from_stream to stream the body back.
    if let Ok(stream) = response.stream() {
        // worker::Response::from_stream takes a stream.
        let mut final_response = Response::from_stream(stream)?;
        final_response = final_response.with_status(response.status_code());
        *final_response.headers_mut() = new_headers;
        Ok(final_response)
    } else {
        // Fallback if no body stream (e.g. null body), sending empty.
        Ok(Response::empty()?
            .with_status(response.status_code())
            .with_headers(new_headers))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_generate_random_ip_format() {
        let ip = generate_random_ip();
        let parts: Vec<&str> = ip.split('.').collect();
        assert_eq!(parts.len(), 4, "IP must have 4 octets: {ip}");

        for part in &parts {
            let octet: u8 = part
                .parse()
                .unwrap_or_else(|_| panic!("Octet '{part}' is not a valid u8 in IP: {ip}"));
            assert!(octet >= 1, "Octet must be >= 1, got {octet} in {ip}");
            // u8 max is 255, so no need to check upper bound explicitly
        }
    }

    #[test]
    fn test_generate_random_ip_nonzero_octets() {
        // Run multiple times to increase confidence
        for _ in 0..10 {
            let ip = generate_random_ip();
            for part in ip.split('.') {
                let octet: u8 = part.parse().expect("valid octet");
                assert!(octet >= 1, "0 is not a valid octet for X-Forwarded-For");
            }
        }
    }

    #[test]
    fn test_filtered_params_contains_expected() {
        assert!(FILTERED_PARAMS.contains(&"url"));
        assert!(FILTERED_PARAMS.contains(&"_cb"));
        assert!(FILTERED_PARAMS.contains(&"_t"));
        assert_eq!(FILTERED_PARAMS.len(), 3);
    }

    #[test]
    fn test_filtered_params_does_not_contain_other() {
        assert!(!FILTERED_PARAMS.contains(&"page"));
        assert!(!FILTERED_PARAMS.contains(&"q"));
        assert!(!FILTERED_PARAMS.contains(&"token"));
    }
}
