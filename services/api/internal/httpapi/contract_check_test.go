package httpapi

import (
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// TestOpenAPIContractMatchesRoutes is the contract-consistency check
// (P2 后端-API: 接口变更坚持先改契约再生成类型）.
//
// It fails when the registered gin routes and the OpenAPI contract disagree
// in either direction:
//   - a route with no contract operation: the interface changed without
//     updating the contract first;
//   - a contract operation with no route: the contract documents something
//     the server does not implement (or a stale entry left after a removal).
// Parameter names are compared exactly: /photos/:angle and /photos/:photoId
// are different operations, not the same route with a renamed segment.
// /healthz and /readyz are in the contract and are checked like any other
// route. Run it with: go test ./internal/httpapi/ -run TestOpenAPIContractMatchesRoutes
func TestOpenAPIContractMatchesRoutes(t *testing.T) {
	raw, err := os.ReadFile("../../../../packages/contracts/openapi.yaml")
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Paths map[string]map[string]yaml.Node `yaml:"paths"`
	}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	contract := map[string]bool{}
	for path, ops := range doc.Paths {
		for method, node := range ops {
			switch method {
			case "get", "post", "put", "delete", "patch":
				var op struct {
					OperationID string `yaml:"operationId"`
				}
				if err := node.Decode(&op); err != nil {
					t.Fatalf("decode %s %s: %v", method, path, err)
				}
				if op.OperationID == "" {
					t.Fatalf("contract op %s %s has no operationId", method, path)
				}
				contract[strings.ToUpper(method)+" "+normalizeContractPath(path)] = true
			}
		}
	}
	routes := map[string]bool{}
	for _, route := range New(&fakeRepository{}, "").Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	var onlyContract, onlyRoute []string
	for key := range contract {
		if !routes[key] {
			onlyContract = append(onlyContract, key)
		}
	}
	for key := range routes {
		if !contract[key] {
			onlyRoute = append(onlyRoute, key)
		}
	}
	sort.Strings(onlyContract)
	sort.Strings(onlyRoute)
	if len(onlyContract) > 0 || len(onlyRoute) > 0 {
		t.Fatalf("contract and routes disagree (contract ops: %d, engine routes: %d):\ncontract op with no route (%d):\n%s\nroute with no contract op (%d):\n%s",
			len(contract), len(routes), len(onlyContract), strings.Join(onlyContract, "\n"), len(onlyRoute), strings.Join(onlyRoute, "\n"))
	}
}

var contractParamPattern = regexp.MustCompile(`\{([^}]+)\}`)

func normalizeContractPath(path string) string {
	return contractParamPattern.ReplaceAllString(path, ":$1")
}
