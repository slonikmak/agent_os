package materials

import "errors"

// ErrUnsupported leaves existing saved-workspace reads usable on platforms
// without a verified confined-handle implementation. Upload stays unadvertised.
var ErrUnsupported = errors.New("material platform unsupported")
