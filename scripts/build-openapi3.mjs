import fs from 'node:fs'
import path from 'node:path'
import process from 'node:process'

const root = process.cwd()
const swaggerPath = path.join(root, 'docs', 'swagger.json')
const openapiPath = path.join(root, 'docs', 'openapi.json')
const swagger = JSON.parse(fs.readFileSync(swaggerPath, 'utf8'))

const app = 'hyperstrate_server_internal_modules_router_application'
const domain = 'hyperstrate_server_internal_modules_router_domain'
const featureTypeRef = `#/components/schemas/${domain}.RouterFeatureType`

const featureConfigs = [
  ['token_optimization', 'FeatureTokenOptimizationConfig'],
  ['context_trimming', 'FeatureContextTrimmingConfig'],
  ['token_cost_optimization', 'FeatureTokenCostOptimizationConfig'],
  ['prompt_optimizer', 'FeaturePromptOptimizerConfig'],
  ['prompt_policy_rollout', 'FeaturePromptPolicyRolloutConfig'],
  ['response_cache', 'FeatureResponseCacheConfig'],
  ['semantic_cache', 'FeatureSemanticCacheConfig'],
  ['retry', 'FeatureRetryConfig'],
  ['rate_limit', 'FeatureRateLimitConfig'],
  ['budget', 'FeatureBudgetConfig'],
  ['fallback', 'EmptyFeatureConfig'],
  ['mcp_tools', 'FeatureMCPToolsConfig'],
  ['health_check', 'EmptyFeatureConfig'],
  ['structured_output', 'FeatureStructuredOutputConfig'],
  ['request_coalescing', 'FeatureRequestCoalescingConfig'],
  ['prompt_caching', 'EmptyFeatureConfig'],
  ['hedging', 'FeatureHedgingConfig'],
  ['quality_gate', 'FeatureQualityGateConfig'],
  ['context_compression', 'FeatureContextCompressionConfig'],
  ['semantic_memory', 'FeatureSemanticMemoryConfig'],
  ['cost_aware_routing', 'FeatureCostAwareRoutingConfig'],
  ['response_prefetch', 'FeatureResponsePrefetchConfig'],
  ['response_fingerprinting', 'FeatureResponseFingerprintingConfig'],
]

const configSchemas = {
  ScopedBudget: object({
    period: string(),
    max_requests: int(),
    max_cost_usd: number(),
  }),
  CostAwareThreshold: object({
    max_chars: int(),
    target_id: string(),
  }),
  PromptOptimizerProtectedTag: object({
    start: string(),
    end: string(),
  }),
  PromptPolicyRolloutVariant: object({
    name: string(),
    prompt_id: string(),
    promptId: string(),
    percentage: number(),
  }),
  EmptyFeatureConfig: object({}),
  FeatureTokenOptimizationConfig: object({ max_chars: int() }),
  FeatureContextTrimmingConfig: object({ max_chars: int() }),
  FeatureTokenCostOptimizationConfig: object({
    fields: stringArray(),
    minify_json: bool(),
    collapse_blank_lines: bool(),
    compact_whitespace: bool(),
    dedupe_lines: bool(),
    max_chars: int(),
    max_prompt_chars: int(),
    output_max_tokens: int(),
    rewrite_model_id: string(),
    rewrite_min_chars: int(),
    rewrite_target_chars: int(),
    rewrite_target_ratio: number(),
  }),
  FeaturePromptOptimizerConfig: object({
    fields: stringArray(),
    optimizers: stringArray(),
    protected_tags: array(ref(`${app}.PromptOptimizerProtectedTag`)),
  }),
  FeaturePromptPolicyRolloutConfig: object({
    variants: array(ref(`${app}.PromptPolicyRolloutVariant`)),
  }),
  FeatureResponseCacheConfig: object({ ttl_seconds: int() }),
  FeatureSemanticCacheConfig: object({
    ttl_seconds: int(),
    similarity_threshold: number(),
    model_id: string(),
  }),
  FeatureRetryConfig: object({
    max_retries: int(),
    initial_delay_ms: int(),
    backoff_multiplier: number(),
  }),
  FeatureRateLimitConfig: object({ rps: number(), burst: int() }),
  FeatureBudgetConfig: object({
    period: string(),
    max_requests: int(),
    max_cost_usd: number(),
    alert_percent: number(),
    agent_budgets: stringMap(ref(`${app}.ScopedBudget`)),
    role_budgets: stringMap(ref(`${app}.ScopedBudget`)),
    repo_budgets: stringMap(ref(`${app}.ScopedBudget`)),
    branch_budgets: stringMap(ref(`${app}.ScopedBudget`)),
  }),
  FeatureMCPToolsConfig: object({
    server_ids: stringArray(),
    max_turns: int(),
    require_approval: bool(),
    allowed_tools: stringArray(),
    blocked_tools: stringArray(),
    allowed_team_ids: stringArray(),
  }),
  FeatureStructuredOutputConfig: object({
    schema: { type: 'object', additionalProperties: true },
    name: string(),
    strict: bool(),
  }),
  FeatureRequestCoalescingConfig: object({
    window_ms: int(),
    max_waiters: int(),
  }),
  FeatureHedgingConfig: object({
    quality_check: string(),
    target_ids: stringArray(),
    targets: stringArray(),
    min_length: int(),
    timeout_ms: int(),
  }),
  FeatureQualityGateConfig: object({
    judge_model_id: string(),
    min_score: number(),
    action: string(),
    rubric_prompt: string(),
    retry_target_id: string(),
  }),
  FeatureContextCompressionConfig: object({
    max_chars: int(),
    keep_recent: int(),
  }),
  FeatureSemanticMemoryConfig: object({
    model_id: string(),
    max_examples: int(),
    ttl_days: int(),
    similarity_threshold: number(),
  }),
  FeatureCostAwareRoutingConfig: object({
    thresholds: array(ref(`${app}.CostAwareThreshold`)),
    default_target_id: string(),
  }),
  FeatureResponsePrefetchConfig: object({
    follow_up_prompts: stringArray(),
    ttl_seconds: int(),
  }),
  FeatureResponseFingerprintingConfig: object({
    window_size: int(),
    alert_threshold: number(),
  }),
}

const openapi = {
  openapi: '3.0.3',
  info: swagger.info,
  servers: buildServers(swagger),
  paths: convertPaths(swagger.paths ?? {}),
  components: {
    schemas: convertRefs(swagger.definitions ?? {}),
    securitySchemes: convertSecurity(swagger.securityDefinitions ?? {}),
  },
  security: swagger.security,
  tags: swagger.tags,
}

for (const [name, schema] of Object.entries(configSchemas)) {
  openapi.components.schemas[`${app}.${name}`] = schema
}

injectFeatureSchemas(openapi.components.schemas)

fs.writeFileSync(openapiPath, `${JSON.stringify(openapi, null, 2)}\n`)

function injectFeatureSchemas(schemas) {
  const addRefs = []
  const responseRefs = []
  const configRefs = []
  const addMapping = {}
  const responseMapping = {}

  for (const [featureType, configName] of featureConfigs) {
    const suffix = pascal(featureType)
    const configRef = ref(`${app}.${configName}`)
    const addName = `${app}.Add${suffix}FeatureInput`
    const responseName = `${app}.${suffix}FeatureResponse`
    const addRef = ref(addName)
    const responseRef = ref(responseName)

    schemas[addName] = object(
      {
        featureType: { type: 'string', enum: [featureType] },
        config: configRef,
        executionOrder: int(),
      },
      ['featureType'],
    )
    schemas[responseName] = object(
      {
        id: string(),
        routerId: string(),
        featureType: { type: 'string', enum: [featureType] },
        config: configRef,
        executionOrder: int(),
        isEnabled: bool(),
        createdAt: { type: 'string', format: 'date-time' },
        modifiedAt: { type: 'string', format: 'date-time' },
      },
      ['id', 'routerId', 'featureType', 'config', 'executionOrder', 'isEnabled', 'createdAt', 'modifiedAt'],
    )

    addRefs.push(addRef)
    responseRefs.push(responseRef)
    configRefs.push(configRef)
    addMapping[featureType] = `#/components/schemas/${addName}`
    responseMapping[featureType] = `#/components/schemas/${responseName}`
  }

  schemas[`${app}.AddFeatureInput`] = {
    oneOf: addRefs,
    discriminator: { propertyName: 'featureType', mapping: addMapping },
  }
  schemas[`${app}.RouterFeatureResponse`] = {
    oneOf: responseRefs,
    discriminator: { propertyName: 'featureType', mapping: responseMapping },
  }
  if (schemas[`${app}.UpdateFeatureInput`]) {
    schemas[`${app}.UpdateFeatureInput`].properties ??= {}
    schemas[`${app}.UpdateFeatureInput`].properties.config = { oneOf: configRefs }
  }
}

function convertPaths(paths) {
  const out = {}
  for (const [route, item] of Object.entries(paths)) {
    out[route] = {}
    for (const [method, operation] of Object.entries(item)) {
      if (method === 'parameters') {
        out[route][method] = convertParameters(operation).parameters
        continue
      }
      const converted = convertRefs({ ...operation })
      delete converted.consumes
      delete converted.produces
      const params = convertParameters(converted.parameters ?? [])
      converted.parameters = params.parameters
      if (params.requestBody) {
        converted['x-codegen-request-body-name'] = 'body'
        converted.requestBody = params.requestBody
      }
      converted.responses = convertResponses(converted.responses ?? {})
      out[route][method] = converted
    }
  }
  return out
}

function convertParameters(parameters) {
  const out = []
  let requestBody
  for (const parameter of parameters) {
    if (parameter.in === 'body') {
      requestBody = {
        required: parameter.required ?? true,
        description: parameter.description,
        'x-codegen-request-body-name': 'body',
        content: {
          'application/json': {
            schema: convertRefs(parameter.schema ?? {}),
          },
        },
      }
      continue
    }
    const converted = convertRefs({ ...parameter })
    const schema = converted.schema ?? primitiveSchema(converted)
    delete converted.type
    delete converted.format
    delete converted.items
    delete converted.collectionFormat
    converted.schema = schema
    out.push(converted)
  }
  return { parameters: out, requestBody }
}

function convertResponses(responses) {
  const out = {}
  for (const [status, response] of Object.entries(responses)) {
    const converted = convertRefs({ ...response })
    if (converted.schema) {
      converted.content = {
        'application/json': {
          schema: converted.schema,
        },
      }
      delete converted.schema
    }
    out[status] = converted
  }
  return out
}

function convertSecurity(securityDefinitions) {
  const out = {}
  for (const [name, scheme] of Object.entries(securityDefinitions)) {
    out[name] = convertRefs({ ...scheme })
  }
  return out
}

function convertRefs(value) {
  if (Array.isArray(value)) return value.map(convertRefs)
  if (!value || typeof value !== 'object') return value
  const out = {}
  for (const [key, inner] of Object.entries(value)) {
    if (key === '$ref' && typeof inner === 'string') {
      out[key] = inner.replace('#/definitions/', '#/components/schemas/')
    } else {
      out[key] = convertRefs(inner)
    }
  }
  return out
}

function buildServers(spec) {
  const schemes = spec.schemes?.length ? spec.schemes : ['http']
  const host = spec.host || 'localhost:8080'
  const basePath = spec.basePath || ''
  return schemes.map((scheme) => ({ url: `${scheme}://${host}${basePath}` }))
}

function primitiveSchema(parameter) {
  if (parameter.type === 'array') return { type: 'array', items: parameter.items ?? {} }
  if (parameter.type) return { type: parameter.type, format: parameter.format }
  return {}
}

function object(properties, required = []) {
  return { type: 'object', properties, ...(required.length ? { required } : {}) }
}

function ref(name) {
  return { $ref: `#/components/schemas/${name}` }
}

function string() {
  return { type: 'string' }
}

function int() {
  return { type: 'integer' }
}

function number() {
  return { type: 'number' }
}

function bool() {
  return { type: 'boolean' }
}

function array(items) {
  return { type: 'array', items }
}

function stringArray() {
  return array(string())
}

function stringMap(additionalProperties) {
  return { type: 'object', additionalProperties }
}

function pascal(value) {
  return value
    .split('_')
    .filter(Boolean)
    .map((part) => `${part[0].toUpperCase()}${part.slice(1)}`)
    .join('')
}
