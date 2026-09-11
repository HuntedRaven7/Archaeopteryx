package machine

import (
	"testing"

	"gopkg.in/yaml.v3"
)

const fixtureAdminConf = `apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCg==
    server: https://127.0.0.1:6443
  name: local
contexts:
- context:
    cluster: local
    user: admin
  name: admin@local
current-context: admin@local
users:
- name: admin
  user:
    client-certificate-data: LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCg==
    client-key-data: LS0tLS1CRUdJTiBQUklWQVRFIEtFWS0tLS0tCg==
`

func TestParseKubeconfig(t *testing.T) {
	kc, err := parseKubeconfig([]byte(fixtureAdminConf))
	if err != nil {
		t.Fatalf("parseKubeconfig: %v", err)
	}
	if len(kc.Clusters) != 1 || kc.Clusters[0].Cluster.Server != "https://127.0.0.1:6443" {
		t.Errorf("clusters = %+v", kc.Clusters)
	}
	if kc.Users[0].User.ClientKeyData == "" {
		t.Error("missing client key data")
	}
}

func TestParseKubeconfigIncomplete(t *testing.T) {
	if _, err := parseKubeconfig([]byte("apiVersion: v1\nkind: Config\n")); err == nil {
		t.Fatal("expected error for incomplete kubeconfig")
	}
}

func TestNodeListReady(t *testing.T) {
	notReady := `{"items":[{"status":{"conditions":[{"type":"Ready","status":"False"}]}}]}`
	if r, err := nodeListReady([]byte(notReady)); err != nil || r {
		t.Errorf("notReady = %v, %v; want false", r, err)
	}

	ready := `{"items":[{"status":{"conditions":[{"type":"Ready","status":"True"},{"type":"MemoryPressure","status":"False"}]}}]}`
	if r, err := nodeListReady([]byte(ready)); err != nil || !r {
		t.Errorf("ready = %v, %v; want true", r, err)
	}

	empty := `{"items":[]}`
	if r, err := nodeListReady([]byte(empty)); err != nil || r {
		t.Errorf("empty = %v, %v; want false", r, err)
	}

	if _, err := nodeListReady([]byte("not json")); err == nil {
		t.Error("expected error for invalid node list")
	}
}

const fixtureFreshKC = `apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: Q0E=
    server: https://127.0.0.1:6443
  name: local
contexts:
- context:
    cluster: local
    user: admin
  name: admin@local
current-context: admin@local
users:
- name: admin
  user:
    client-certificate-data: Q0VSVA==
    client-key-data: S0VZ
`

func TestMergeKubeconfigIdempotent(t *testing.T) {
	existing := []byte("apiVersion: v1\nkind: Config\npreferences: {}\nclusters:\n- cluster:\n    server: https://other:6443\n  name: other\nusers:\n- name: other-user\n  user: {}\ncontexts:\n- context:\n    cluster: other\n    user: other-user\n  name: other\ncurrent-context: other\n")

	merged, err := MergeKubeconfig(existing, []byte(fixtureFreshKC), "10.0.0.5")
	if err != nil {
		t.Fatalf("MergeKubeconfig: %v", err)
	}
	// Re-merge must not duplicate entries.
	merged2, err := MergeKubeconfig(merged, []byte(fixtureFreshKC), "10.0.0.5")
	if err != nil {
		t.Fatalf("re-merge: %v", err)
	}
	for _, blob := range [][]byte{merged, merged2} {
		kc, err := parseKubeconfig(blob)
		if err != nil {
			t.Fatalf("merged kubeconfig unparseable: %v\n%s", err, blob)
		}
		_ = kc
	}
	if count := len(kubeSlice(asMap(merged2), "contexts")); count != 2 {
		t.Errorf("re-merged contexts = %d, want 2", count)
	}
	if count := len(kubeSlice(asMap(merged2), "clusters")); count != 2 {
		t.Errorf("re-merged clusters = %d, want 2", count)
	}
	if count := len(kubeSlice(asMap(merged2), "users")); count != 2 {
		t.Errorf("re-merged users = %d, want 2", count)
	}
}

func asMap(b []byte) map[string]any {
	var m map[string]any
	_ = yaml.Unmarshal(b, &m)
	return m
}

func TestMergeKubeconfigIncomplete(t *testing.T) {
	if _, err := MergeKubeconfig([]byte("{}"), []byte("apiVersion: v1\nkind: Config\n"), "x"); err == nil {
		t.Fatal("expected error for incomplete fresh kubeconfig")
	}
	if _, err := MergeKubeconfig([]byte("{}"), []byte(fixtureFreshKC), ""); err == nil {
		t.Fatal("expected error for empty context name")
	}
}
