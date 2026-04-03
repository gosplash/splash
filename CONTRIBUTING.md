# Contributing to Splash

Thank you for your interest in contributing to Splash!

Splash is a compiler with a **working safety frontend** focused on making unsafe AI agent systems fail to compile. The core effect system, call-graph analysis, data classification, `@redline`, `@sandbox`, `@approve`, and `@tool` schema generation are already implemented and functional.

We welcome contributions of all sizes — from fixing error messages to adding examples to helping build out the standard library.

## Current Status & Roadmap

See [ROADMAP.md](ROADMAP.md) for the latest project status, completed phases, and upcoming priorities.

In short:
- **Done**: Parser, type checker, effect system, call-graph analysis, safety annotations (`@redline`, `@sandbox`, `@approve`, `@containment`), data classification (`@sensitive`/`@restricted`), `@tool` JSON Schema generation, `splash check`, `splash tools`, and `splash build`.
- **In progress / Next**: Standard library adapters (`std/db`, `std/ai` runtime, `std/http`, etc.), in-memory dev mode, and full end-to-end runtime experience.

## Ways to Contribute

### High-Leverage Areas
These align with the current priorities in [ROADMAP.md](ROADMAP.md):

- Improving the parser, type checker, and effect system (especially error messages and diagnostics)
- Refining call-graph analysis and agent reachability enforcement
- Enhancing data classification handling for complex types
- Improving `@tool` JSON Schema generation (nested types, enums, optionals, etc.)
- Writing and polishing examples (see `examples/whoop/whoop_api.splash`)
- Adding tests for safety checks
- Documentation improvements
- Early work on standard library adapters (in-memory implementations are especially welcome)

### Good First Issues
Look for issues labeled **`good first issue`** or **`help wanted`**. Common starter tasks include:
- Adding new examples or improving existing ones
- Fixing or clarifying error messages
- Small documentation or comment improvements
- Writing unit tests for type checker or effect propagation

### Bug Reports and Feature Requests
- Open an issue with a clear title.
- For bugs, please include:
  - The commit or version of Splash
  - A minimal `.splash` file that reproduces the problem
  - The command you ran (`splash check`, `splash tools`, etc.) and the output

## Development Setup

```bash
git clone https://github.com/gosplash/splash.git
cd splash
go mod download

# Build the CLI
go build -o splash ./cmd/splash

# Try it on the health coach example
./splash check examples/health/health_api.splash
./splash tools examples/health/health_api.splash
```

The compiler is written in Go.

Run tests with go test ./...

Format code with go fmt ./...

## Pull Request Process

1. Fork the repository and create a feature branch.
2 Make your changes and ensure tests pass (go test ./...).
3. Verify that splash check still passes on existing examples.
4. Open a Pull Request with a clear description of the changes.
5. Reference any related issues.

We aim to review PRs promptly. Small, focused changes are especially appreciated.

## Design Changes

For significant changes to the language (new syntax, effect system modifications, safety semantics, etc.), please open an issue first for discussion. Major decisions should align with the principles in the whitepaper and the current ROADMAP.md.Questions?

Feel free to open an issue with the label question or reach out on X (links in the README).

We're excited to build a safer foundation for AI agents together. 

Contributions at any level help move the project forward.

Happy hacking!


