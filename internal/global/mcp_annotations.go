package global

// MCP metadata lives in cobra command Annotations (ADR 0003, decision 3). This
// file is the single place where an annotation key is spelled; commands and the
// MCP server must reference these names and never retype the string.
//
// Only the keys that exist today are declared. Phase 3 of ADR 0003 adds
// hal.mcp/ends-http-session (command) and hal.mcp/hostpath (flag) next to this
// one, and classifies the commands that predate the annotations.

const (
	// AnnotationMCPReadOnly marks a command as safe to run through hal_read: it
	// observes the lab and never changes it. When the annotation is absent the
	// command is treated as an action (hal_run only), which fails safe.
	AnnotationMCPReadOnly = "hal.mcp/readonly"

	// AnnotationValueTrue is the only value the readonly annotation takes. The
	// ADR table spells it as the string "true", because cobra annotations are a
	// map[string]string.
	AnnotationValueTrue = "true"
)

// MCPReadOnlyAnnotations returns a fresh Annotations map marking a command as
// read-only. A new map is returned on every call so that commands never share
// (and never mutate) the same underlying map.
func MCPReadOnlyAnnotations() map[string]string {
	return map[string]string{AnnotationMCPReadOnly: AnnotationValueTrue}
}
