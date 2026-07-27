// foliate-js ships as plain ESM without type declarations. We only use the
// <foliate-view> custom element it registers (via a lazy import) and drive it
// through a few untyped methods (open/goLeft/goRight/close), so a bare module
// shim is enough.
declare module 'foliate-js/view.js'
declare module 'foliate-js/*'
