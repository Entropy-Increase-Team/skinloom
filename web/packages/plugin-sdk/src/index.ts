export * from './gen/catalog.js'
export { definePlugin, reject } from './server/index.js'
export type {
  GuardRejection,
  PluginAuthProviderDef,
  PluginCollection,
  PluginContext,
  PluginDefinition,
  PluginFetchInit,
  PluginFetchResponse,
  PluginHttpRequest,
  PluginHttpResponse,
  PluginRouteAuth,
  PluginRouteOptions,
} from './server/index.js'
export { defineWebPlugin } from './web/index.js'
export type { WebPluginDefinition, WebPluginSlotBinding } from './web/index.js'
