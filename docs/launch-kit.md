# Hyperstrate Launch Kit

Copy-ready positioning, GitHub metadata, and launch posts for the two public Hyperstrate repositories.

## Core Positioning

Main tagline:

Hyperstrate is a self-hosted AI gateway runtime and control plane for routing, governing, and observing production LLM traffic.

Short version:

Open-source AI gateway for production LLM routing, governance, evals, MCP tools, and observability.

Bifrost-style version:

Self-hosted enterprise AI gateway with router pipelines, multi-provider LLM routing, virtual keys, budgets, guardrails, evals, MCP tools, tracing, and SDK-compatible APIs.

## GitHub About Metadata

### Hyperstrate/server

Description:

Self-hosted enterprise AI gateway with model routing, provider orchestration, virtual keys, budgets, guardrails, evals, MCP tools, observability & SDK-compatible APIs.

Topics:

```text
ai-gateway
llm-gateway
llmops
openai-compatible
anthropic
mcp
model-routing
observability
guardrails
evals
rate-limiting
cost-control
go
gin
aws-lambda
postgresql
sqlite
self-hosted
```

### Hyperstrate/client

Description:

Open-source AI gateway control plane with visual router pipelines, provider management, prompts, evals, MCP tools, virtual keys, teams, SSO groups, analytics & observability.

Topics:

```text
ai-gateway
llm-gateway
llmops
control-plane
vue
typescript
vite
tailwindcss
model-routing
observability
analytics
mcp
prompt-management
evals
virtual-keys
self-hosted
```

## GitHub Release Copy

Title:

Hyperstrate v0.1.0

Body:

```markdown
Initial public release of Hyperstrate: a self-hosted AI gateway runtime and control plane for routing, governing, and observing production LLM traffic.

## What's included

- Multi-provider AI gateway runtime
- OpenAI-compatible, Anthropic-compatible, native, streaming, async, and proxy inference flows
- Router pipelines with retry, fallback, caching, rate limits, budgets, quality gates, semantic memory, and MCP tools
- Interceptors for semantic routing, A/B testing, content filtering, PII handling, prompt guard, prompt shield, and team budgets
- Virtual keys, API keys, teams, users, organizations, and OIDC/JWKS group mappings
- Inference logs, pipeline traces, analytics, Prometheus metrics, audit logs, CSV export, webhooks, and request replay
- Prompt templates with versioning
- Evaluation sets and router regression runs
- Vue control plane for managing providers, routers, prompts, evals, MCP tools, teams, keys, and analytics
- Local SQLite development mode and AWS Lambda/PostgreSQL/SQS deployment path

## Good for

- Teams building production LLM apps
- Developers who want one gateway in front of multiple model providers
- Teams that need budgets, rate limits, evals, traces, virtual keys, and model/provider governance
- Self-hosted AI infrastructure setups
```

## Hacker News

Title:

```text
Show HN: Hyperstrate - self-hosted AI gateway for production LLM traffic
```

First comment:

```text
Hey HN,

I built Hyperstrate, an open-source AI gateway runtime and control plane for teams running LLM traffic in production.

The idea is to put one self-hosted gateway in front of providers like OpenAI, Anthropic, Gemini, Mistral, Azure OpenAI, Bedrock, Groq, Cohere, Ollama, vLLM, LocalAI, and custom OpenAI-compatible endpoints.

It includes:

- Router pipelines for retry, fallback, caching, rate limits, budgets, quality gates, semantic memory, and MCP tools
- OpenAI-compatible and Anthropic-compatible proxy routes
- Virtual keys, team access, API keys, OIDC/JWKS group mappings, and spending controls
- Inference logs, pipeline traces, analytics, request replay, webhooks, Prometheus metrics, and CSV export
- Prompt templates, versioning, eval sets, and router regression runs
- A Vue control plane for visually managing providers, routers, prompts, evals, MCP servers, teams, keys, and analytics

I built it because many LLM apps start by hard-coding model providers directly into each service, then later need routing, failover, budgets, logs, evals, and governance bolted on afterward.

Would love feedback from people running LLM traffic in production, especially around the gateway API, router model, and what you would expect from a self-hosted LLM gateway.

Server repo: https://github.com/Hyperstrate/server
Client repo: https://github.com/Hyperstrate/client
```

## Reddit

### r/selfhosted

Title:

```text
I built a self-hosted AI gateway with routing, budgets, evals, MCP tools and observability
```

Post:

```text
I have been working on Hyperstrate, an open-source self-hosted AI gateway and control plane for production LLM traffic.

The goal is to give teams one gateway in front of providers like OpenAI, Anthropic, Gemini, Mistral, Azure OpenAI, Bedrock, Groq, Cohere, Ollama, vLLM, LocalAI, and custom OpenAI-compatible endpoints.

It supports:

- OpenAI-compatible and Anthropic-compatible routes
- Router pipelines with retry, fallback, caching, rate limits, budgets, quality gates, semantic memory, and MCP tools
- Virtual keys, API keys, teams, orgs, and OIDC/JWKS group mappings
- Inference logs, full pipeline traces, analytics, request replay, webhooks, Prometheus metrics, and CSV export
- Prompt templates, eval sets, and router regression runs
- A Vue control plane for configuring providers, routers, prompts, evals, MCP tools, teams, keys, and analytics

It runs locally with SQLite for development, and the server also has a production deployment path using AWS Lambda, PostgreSQL, and SQS.

I would love feedback from other self-hosters: what would make this easier to run, deploy, or trust in your own stack?

Server repo: https://github.com/Hyperstrate/server
Client repo: https://github.com/Hyperstrate/client
```

### r/LocalLLaMA

Title:

```text
Open-source AI gateway for routing between local and hosted LLM providers
```

Post:

```text
I built Hyperstrate, an open-source AI gateway runtime and web control plane for routing LLM traffic across local and hosted providers.

It can sit in front of hosted providers like OpenAI, Anthropic, Gemini, Mistral, Bedrock, Groq, Cohere, etc., but also self-hosted/local endpoints like Ollama, vLLM, LocalAI, or any OpenAI-compatible endpoint.

Useful pieces for local/model-routing workflows:

- Router pipelines with weighted, percentage, failover, random, and latency-based strategies
- Retry, fallback, caching, rate limits, budgets, quality gates, semantic memory, and MCP tools
- OpenAI-compatible proxy routes, so existing SDK integrations can point to the gateway
- Full inference logs, pipeline traces, cost/token/latency analytics, replay, and eval runs
- A Vue control plane for visually managing providers, routers, prompts, evals, MCP tools, virtual keys, and analytics

I am looking for feedback from people running local models or hybrid local/hosted setups. What routing, fallback, observability, or provider-management features would be most useful?

Server repo: https://github.com/Hyperstrate/server
Client repo: https://github.com/Hyperstrate/client
```

## Product Hunt

Product name:

```text
Hyperstrate
```

Tagline:

```text
Open-source AI gateway for production LLM traffic
```

Description:

```text
Hyperstrate is a self-hosted AI gateway runtime and control plane for routing, governing, and observing production LLM traffic. Route requests across hosted and self-hosted model providers, build router pipelines, manage virtual keys and budgets, run evals, connect MCP tools, inspect traces, and monitor usage from a modern Vue control plane.
```

First maker comment:

```text
Hey Product Hunt,

I am launching Hyperstrate, an open-source AI gateway runtime and control plane for teams building production LLM applications.

The problem: many AI apps start by calling OpenAI, Anthropic, Gemini, Ollama, vLLM, or other providers directly. As soon as the app becomes serious, teams need routing, fallback, budgets, rate limits, provider governance, prompt management, evals, logs, traces, and observability.

Hyperstrate gives you one self-hosted gateway in front of your model providers.

It includes:

- Router pipelines for retry, fallback, caching, rate limits, budgets, quality gates, semantic memory, and MCP tools
- OpenAI-compatible and Anthropic-compatible routes
- Provider orchestration across hosted and self-hosted models
- Virtual keys, API keys, teams, orgs, and OIDC/JWKS group mappings
- Prompt templates, versioning, eval sets, and router regression runs
- Inference logs, pipeline traces, analytics, request replay, webhooks, Prometheus metrics, and CSV export
- A Vue control plane for configuring providers, routers, prompts, evals, MCP servers, teams, keys, and analytics

Would love feedback from AI builders, platform engineers, and teams running LLM traffic in production.
```

Gallery image ideas:

- Hero screenshot: visual router pipeline builder
- Provider management screen
- Inference trace / request detail screen
- Analytics dashboard
- Prompt/eval management screen

## Social Posts

### X / Twitter

```text
Launching Hyperstrate: an open-source, self-hosted AI gateway runtime + control plane for production LLM traffic.

Route across OpenAI, Anthropic, Gemini, Mistral, Bedrock, Groq, Cohere, Ollama, vLLM, LocalAI & custom endpoints.

Includes router pipelines, fallbacks, caching, budgets, rate limits, virtual keys, evals, MCP tools, traces, analytics & a Vue control plane.

Server: https://github.com/Hyperstrate/server
Client: https://github.com/Hyperstrate/client
```

### LinkedIn

```text
I am launching Hyperstrate - an open-source, self-hosted AI gateway runtime and control plane for production LLM traffic.

Most AI apps start by calling model providers directly. That works at first, but production teams quickly need routing, failover, budgets, rate limits, evals, traces, virtual keys, team access, prompt management, and observability.

Hyperstrate gives you one gateway in front of providers like OpenAI, Anthropic, Gemini, Mistral, Azure OpenAI, Bedrock, Groq, Cohere, Ollama, vLLM, LocalAI, and custom OpenAI-compatible endpoints.

It includes router pipelines, retry, fallback, caching, budgets, rate limits, MCP tools, virtual keys, teams, OIDC/JWKS group mappings, inference logs, traces, analytics, request replay, Prometheus metrics, webhooks, prompt templates, eval sets, and a Vue control plane.

I would love feedback from AI engineers, platform teams, and anyone building production LLM infrastructure.

Server repo: https://github.com/Hyperstrate/server
Client repo: https://github.com/Hyperstrate/client
```

## Awesome List Entry

Suggested category:

```text
AI Gateways / LLM Gateways / LLMOps Platforms
```

Entry:

```markdown
- [Hyperstrate](https://github.com/Hyperstrate/server) - Open-source, self-hosted AI gateway runtime and control plane for production LLM traffic. Supports multi-provider routing, OpenAI/Anthropic-compatible APIs, router pipelines, virtual keys, budgets, rate limits, guardrails, evals, MCP tools, traces, analytics, and a Vue control plane.
```

PR description:

```text
Adds Hyperstrate to the AI Gateway / LLMOps section.

Hyperstrate is an open-source, self-hosted AI gateway runtime and control plane for production LLM traffic. It provides multi-provider routing, SDK-compatible proxy APIs, virtual keys, budgets, rate limits, guardrails, evals, MCP tools, request traces, analytics, and a Vue-based control plane.
```

## Launch Checklist

1. Add the GitHub About descriptions to both repos.
2. Add the topics listed above to both repos.
3. Add or refresh screenshots/GIFs, especially for the client.
4. Make sure both READMEs cross-link the server and client repos.
5. Create v0.1.0 releases for both repos.
6. Pin a GitHub issue titled `Roadmap / feedback`.
7. Post Show HN.
8. Post to one subreddit first, then wait and respond.
9. Publish the article version on Dev.to or Hashnode.
10. Post the LinkedIn and X versions.
11. Submit PRs or issues to relevant awesome lists.
12. Launch on Product Hunt once screenshots and a landing page are ready.
