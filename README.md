# Proxyflare

![License](https://img.shields.io/badge/license-MIT-blue.svg)
![Python Version](https://img.shields.io/badge/python-3.12%2B-blue.svg)
![Rust](https://img.shields.io/badge/rust-supported-orange.svg)
![Coverage](https://img.shields.io/badge/coverage-83%25-brightgreen.svg)

<div align="center">
  <b>English</b> | <a href="README.ru.md">Русский</a>
</div>

---

> CLI utility for managing proxy servers based on Cloudflare Workers

## 📋 Description

**Proxyflare** is a modern CLI utility for creating and managing proxy workers on the Cloudflare Workers platform. The project is a complete rewrite of the existing `flareprox.py` script, utilizing best practices for Python and Rust development.

### Key Features

- **Multi-platform**: Support for workers written in Python, JavaScript, and Rust (WASM).
- **Parallel Deployment**: Quickly create dozens of workers simultaneously.
- **Flexible Routing**: Route targets via query parameters, headers, or URL path.
- **Local Client**: Built-in tools for testing and interacting with the worker pool.
- **Artifact Management**: Automatic compilation of Rust workers and resource copying.

---

## ⚖️ Comparison with Original flareprox

> [!NOTE]
> The **Proxyflare** project is inspired by the [flareprox](https://github.com/MrTurvey/flareprox) utility, but offers several significant architectural improvements:

| Feature               | flareprox (Original)                  | Proxyflare                                                                                                |
| :-------------------- | :------------------------------------ | :-------------------------------------------------------------------------------------------------------- |
| **Worker Languages**  | JavaScript only                       | **JavaScript, Python, Rust (WASM)**                                                                       |
| **Architecture**      | Monolithic Python script (~300 lines) | Modular CLI application (Typer, Rich, Pydantic)                                                           |
| **Client Interface**  | Returns raw URLs                      | Built-in custom `httpx` Transport for automatic transparent proxying and ProxyManager for pool management |
| **Testing**           | None                                  | Full Unit coverage and integration tests with `wrangler dev` + E2E                                        |
| **Worker Management** | Synchronous creation                  | Parallel creation, deletion, and pool listing (batch operations)                                          |
| **Artifact Build**    | JS hardcoded inside Python            | Separated into individual files, Automatic dynamic Rust build via `cargo/worker-build`                    |

---

## ⚡ Worker Performance Benchmark

Stress testing results (200 requests with `concurrency: 20` via Cloudflare) for each worker type:

| Worker Type     | Latency (avg) | Max Latency  | Requests per Second (RPS) |
| :-------------- | :------------ | :----------- | :------------------------ |
| **JS**          | ~425 ms       | ~1432 ms     | ~43 RPS                   |
| **Python**      | ~410 ms       | ~1298 ms     | ~46 RPS                   |
| **Rust (WASM)** | **~240 ms**   | **~1119 ms** | **~73 RPS**               |

*ℹ️ Benchmark was conducted from a local machine*  
*ℹ️ Results may vary depending on your region and network load*  
TODO: benchmark locally via `wrangler dev` 
*ℹ️ Rust workers show the best performance and lowest latency thanks to WebAssembly, making them ideal for high-load scraping/proxy tasks.*  

---

## 🏗️ Architecture and Workflow

### CLI Utility
1. **Configuration**: Manage Cloudflare settings via `.env` or system variables.
2. **Worker Management**:
    * `config verify`: Verify API token and access permissions.
    * `config show`: View current configuration.
    * `create`: Parallel deployment of workers.
    * `list`: List all workers with the `proxyflare` prefix.
    * `delete`: Delete workers by name or mask.
    * `test`: Test proxy functionality via the local client.

### Client Library ([README](src/proxyflare/client/README.md))
1. **Manager**: Manage the list of active workers and select a random node.
2. **Transport**: Custom transport for `httpx` to transparently proxy requests through workers.

### Workers (Proxy)
1. **Compatibility**: Header stripping (Cloudflare-specific, Host) for correct proxying.
2. **CORS**: Built-in CORS support (`Access-Control-Allow-Origin: *`).
3. **Performance**: Optimized Rust worker for high-load tasks.

## Tech Stack

### Python Stack
- **Python 3.12**+
- **UV** - for dependency management
- **Cloudflare SDK** - API interaction
- **Typer & Rich** - modern CLI interface
- **Pydantic V2** - data models and settings
- **Loguru** - structured logging
- **Tenacity** - resilient network operations

### Rust Stack
- **Rust** & **Cargo** — compilation of high-performance workers
- **worker-rs** — Cloudflare Workers SDK for Rust

---

## 📦 Project Structure

```
proxyflare/
├── src/
│   └── proxyflare/
│       ├── cli/                 # CLI interface and commands
│       ├── client/              # Client library (httpx transport)
│       ├── models/              # Pydantic models (Config, Results)
│       ├── services/            # Business logic (WorkerService, Tester)
│       ├── workers/             # Worker sources (py, js, rs)
│       └── utils/               # Utilities (artifacts, markers)
│
└── tests/                       # Full test coverage (Unit, Integration, E2E)
```

---

## 🛠️ Installation and Usage

### Requirements
- **Python 3.12+**
- **Rust (cargo)** (for compiling WASM artifacts, the `worker-build` plugin gets installed automatically)
- **Node.js (npm/npx)** (for running local tests via `wrangler dev`)

### Cloudflare API Token Setup

To use **Proxyflare**, you need to provide a custom Cloudflare API token. Create a custom token in your Cloudflare dashboard (`My Profile -> API Tokens -> Create Token -> Custom Token`) with the following **four** specific permissions:

1. **User** -> **API Tokens** -> **Read**
2. **Account** -> **Workers Scripts** -> **Edit**
3. **Account** -> **Account Settings** -> **Read**
4. **Zone** -> **Workers Routes** -> **Edit**

> [!WARNING]
> Do not restrict the token to specific zones in "Zone Resources" or "Account Resources". Set them to **Include -> All accounts** and **Include -> All zones**, otherwise the `verify` endpoint will return an `1000 Invalid API Token` error.

Set the token and your Account ID as environment variables (e.g., in a `.env` file):
```bash
PROXYFLARE_API_TOKEN="your_custom_token"
PROXYFLARE_ACCOUNT_ID="your_account_id"
```

### Installation and Build for Development

The project uses a custom `hatchling` build hook, which automatically compiles Rust workers upon package installation. It is recommended to use `uv` for fast dependency management and utility building:

```bash
# Install project dependencies
uv sync

# Install as a system utility tool
uv tool install .

# Install package in development mode (triggers Rust worker build)
uv pip install -e .
```

After installation, you can verify your configuration with the built-in command:
```bash
proxyflare config verify
```

### Development and Testing

1. **Unit tests** (Fast, local):
```bash
uv run pytest tests/unit
```

2. **Integration tests** (Local wrangler):
```bash
uv run pytest tests/integration
```

3. **E2E tests** (Remote Cloudflare):
```bash
uv run pytest tests/remote/test_e2e.py
```

---

## 📝 License

MIT

---

**Status:** 🟢 Stable version (Core Ready) | **Current version:** 0.1.0
