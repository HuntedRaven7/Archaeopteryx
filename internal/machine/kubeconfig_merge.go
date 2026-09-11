package machine

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// KubeconfigAdmin returns `k0s kubeconfig admin` output.
func KubeconfigAdmin() ([]byte, error) {
	if _, err := execLookPath("k0s"); err != nil {
		return nil, fmt.Errorf("k0s not available: %w", err)
	}
	out, err := execCommand("k0s", "kubeconfig", "admin").CombinedOutput()
	return out, err
}

// findKubeEntries returns the slices for the given key if present.
func kubeSlice(m map[string]any, key string) []any {
	if v, ok := m[key].([]any); ok {
		return v
	}
	return nil
}

// mergeKubeconfig rewrites `fresh` under the name prefix `apx-<name>` and
// merges it into the generic map `target`. Entries already carrying the same
// prefix are replaced, so re-merging is idempotent. The merged context becomes
// the current one. The map preserves every field k0s emitted.
func mergeKubeconfig(target map[string]any, fresh map[string]any, name string) error {
	if name == "" {
		return fmt.Errorf("context name is required")
	}
	ctxName := "apx-" + name

	// Normalize fresh document to a map (k0s emits the standard shape).
	clusters := kubeSlice(fresh, "clusters")
	contexts := kubeSlice(fresh, "contexts")
	users := kubeSlice(fresh, "users")
	if len(clusters) == 0 || len(contexts) == 0 || len(users) == 0 {
		return fmt.Errorf("kubeconfig is incomplete (missing clusters/contexts/users)")
	}

	// Reject names on a single entry each (standard k0s admin.conf).
	cluster := clusters[0].(map[string]any)
	context := contexts[0].(map[string]any)
	user := users[0].(map[string]any)

	cluster["name"] = ctxName
	user["name"] = ctxName
	context["name"] = ctxName
	if c, ok := context["context"].(map[string]any); ok {
		c["cluster"] = ctxName
		c["user"] = ctxName
	}

	// Drop previous entries with our name so merges stay idempotent.
	target["clusters"] = replaceNamed(kubeSlice(target, "clusters"), cluster, ctxName)
	target["contexts"] = replaceNamed(kubeSlice(target, "contexts"), context, ctxName)
	target["users"] = replaceNamed(kubeSlice(target, "users"), user, ctxName)
	target["current-context"] = ctxName
	return nil
}

// replaceNamed removes every entry named n and appends add.
func replaceNamed(entries []any, add map[string]any, n string) []any {
	out := make([]any, 0, len(entries)+1)
	for _, e := range entries {
		m, ok := e.(map[string]any)
		if ok && m["name"] == n {
			continue
		}
		out = append(out, e)
	}
	return append(out, add)
}

// MergeKubeconfig merges fresh kubeconfig bytes into existing kubeconfig bytes
// under the apx-<name> identity prefix, returning the combined document. An
// empty existing config starts a new one.
func MergeKubeconfig(existing, fresh []byte, name string) ([]byte, error) {
	var target map[string]any
	if err := yaml.Unmarshal(existing, &target); err != nil {
		return nil, fmt.Errorf("parse existing kubeconfig: %w", err)
	}
	if target == nil {
		target = map[string]any{}
	}
	var incoming map[string]any
	if err := yaml.Unmarshal(fresh, &incoming); err != nil {
		return nil, fmt.Errorf("parse fresh kubeconfig: %w", err)
	}
	if err := mergeKubeconfig(target, incoming, name); err != nil {
		return nil, err
	}
	// New documents inherit the standard header from the incoming config.
	if target["apiVersion"] == nil {
		target["apiVersion"] = incoming["apiVersion"]
	}
	if target["kind"] == nil {
		target["kind"] = incoming["kind"]
	}
	out, err := yaml.Marshal(target)
	if err != nil {
		return nil, fmt.Errorf("marshal merged kubeconfig: %w", err)
	}
	return out, nil
}
