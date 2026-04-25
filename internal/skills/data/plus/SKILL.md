---
name: plus
description: Deploy, configure, and operate HAL Plus — the local AI-powered web UI layer for HAL. Use when the user asks to start HAL Plus, switch models, check its health, or understand how it connects to Ollama and HAL MCP.
---

# HAL Plus

HAL Plus is the local web UI for HAL. It runs as a container alongside HAL MCP and proxies queries to an Ollama model on the host. Answers are grounded through the HAL MCP runtime tools.

## Lab Assumptions

- Ollama must be running on the host (`ollama serve`) before `hal plus create`.
- HAL Plus connects to Ollama via `host.containers.internal:11434` from inside the container.
- HAL MCP (`hal mcp serve`) runs alongside HAL Plus on the same container network.
- Default model preset: `qwen3.5` (32k context window).

## Primary Commands

```
hal plus create
hal plus status
hal plus delete
```

## Model Presets

```
hal plus create --model qwen3.5      # default — balanced, 32k context
hal plus create --model gemma4       # lighter, faster cold starts
```

Both presets are HAL-managed — no manual `ollama pull` needed.

## Custom Model or Modelfile

```
hal plus create --model qwen3.5 --keep-alive 5m
hal plus create --model qwen3.5 --model-config ./Modelfile
```

## Custom Image

```
hal plus create --image localhost/hal-plus:testmiche
```

Use a locally built image (e.g. during development or testing a branch).

## Lab Endpoints

| Service | URL |
|---|---|
| HAL Plus UI | http://hal.localhost:9000 |
| HAL Plus UI (dark mode) | http://hal.localhost:9000/dark |
| HAL Plus API | http://hal.localhost:9000/api |

## Architecture Notes

- HAL Plus API server runs on port 9001 internally; HAL forwards port 9000 → 9001.
- HAL MCP is started as a sibling container and reached via HTTP at `http://127.0.0.1:18080/mcp` from inside HAL Plus.
- Answers are classified by intent route:
  - **Route A** (knowledge/factual): short prose + one command + one doc
  - **Route B** (operational): Preflight → Run → Check → Verify → Docs — Qwen wraps the MCP-grounded block in humanized prose
  - **Route C** (follow-up): prior matched behavior context injected as model grounding
- Status/health checks bypass the Qwen wrapping step and stream the deterministic answer directly.

## Edge Cases

- If HAL Plus reports `LLM offline`, ensure Ollama is running (`ollama serve`) and the model is present (`ollama list`).
- If MCP tools are missing, run `hal mcp status` to confirm the MCP server is reachable.
- If `hal plus create` fails with an image pull error, either push the image to a registry or use `--image localhost/hal-plus:local` with a locally built image.
- `hal plus delete` removes the container but does NOT unload the Ollama model from the host.
