package tree_sitter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/go-logr/logr"
	base "github.com/konveyor/analyzer-lsp/lsp/base_service_client"
	"github.com/konveyor/analyzer-lsp/lsp/protocol"
	"github.com/konveyor/analyzer-lsp/provider"
	"github.com/swaggest/openapi-go/openapi3"
	"go.lsp.dev/uri"
	"gopkg.in/yaml.v2"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
	tree_sitter_java "github.com/tree-sitter/tree-sitter-java/bindings/go"
)

// **DELETE THIS COMMENT BLOCK FOR NEW SERVICE CLIENTS**
//
// Suppose the name of your language server is `foo-lsp`. The recommended
// pattern of adding new service clients is:
//
// 1. Create a new folder in `server_configurations` with the name of your lsp
//    server, `foo_lsp`. Copy `generic/service_client.go` into the new directory.
//
// 2. Change the package name to `foo_lsp`. Change the occurrences of
//    `GenericServiceClient` to `FooServiceClient`.
//
// 3. Add any variables you need to the `FooServiceClient` and
//    `FooServiceClientConfig` struct.
//
// 4. Modify any parameters related to the `initialize` request. If you need
//    additional `jsonrpc2_v2` handlers, say for responding to messages from the
//    server in a specific way, pass those into the base service client.
//
// 5. Implement your capabilities and update the `FooServiceClientCapabilities`
//    slice
//
// 6. In constants.go, add `NewFooServiceClient` to SupportedLanguages and
//    `FooServiceClientCapabilities` to SupportedCapabilities

type TreeSitterServiceClientConfig struct {
	base.LSPServiceClientConfig `yaml:",inline"`

	// Add any additional fields you need here
}

// Tidy aliases
type serviceClientFn = base.LSPServiceClientFunc[*TreeSitterServiceClient]

type TreeSitterServiceClient struct {
	*base.LSPServiceClientBase
	*base.LSPServiceClientEvaluator[*TreeSitterServiceClient]

	Config TreeSitterServiceClientConfig

	// Add any additional fields you need here

	Location            string
	TreeSitterLanguages map[string]*tree_sitter.Language
	ExtMap              map[string]string

	// Technically a memory leak
	NodeCache    map[string]*tree_sitter.Node
	LastModified map[string]time.Time
}

type TreeSitterServiceClientBuilder struct{}

func (g *TreeSitterServiceClientBuilder) Init(ctx context.Context, log logr.Logger, c provider.InitConfig) (provider.ServiceClient, error) {
	sc := &TreeSitterServiceClient{}

	// Unmarshal the config
	b, _ := yaml.Marshal(c.ProviderSpecificConfig)
	err := yaml.Unmarshal(b, &sc.Config)
	if err != nil {
		return nil, fmt.Errorf("generic providerSpecificConfig Unmarshal error: %w", err)
	}

	// Create the parameters for the `initialize` request
	//
	// TODO(jsussman): Support more than one folder. This hack with only taking
	// the first item in WorkspaceFolders is littered throughout.
	params := protocol.InitializeParams{}

	if c.Location != "" {
		sc.Config.WorkspaceFolders = []string{c.Location}
	}

	if len(sc.Config.WorkspaceFolders) == 0 {
		params.RootURI = ""
	} else {
		params.RootURI = sc.Config.WorkspaceFolders[0]
	}

	params.Capabilities = protocol.ClientCapabilities{}

	var InitializationOptions map[string]any
	err = json.Unmarshal([]byte(sc.Config.LspServerInitializationOptions), &InitializationOptions)
	if err != nil {
		// fmt.Printf("Could not unmarshal into map[string]any: %s\n", sc.Config.LspServerInitializationOptions)
		params.InitializationOptions = map[string]any{}
	} else {
		params.InitializationOptions = InitializationOptions
	}

	// Initialize the base client
	// scBase, err := base.NewLSPServiceClientBase(
	// 	ctx, log, c,
	// 	base.LogHandler(log),
	// 	params,
	// )
	// if err != nil {
	// 	return nil, fmt.Errorf("base client initialization error: %w", err)
	// }
	sc.LSPServiceClientBase = &base.LSPServiceClientBase{}

	// Initialize the fancy evaluator (dynamic dispatch ftw)
	eval, err := base.NewLspServiceClientEvaluator[*TreeSitterServiceClient](sc, g.GetGenericServiceClientCapabilities(log))
	if err != nil {
		return nil, fmt.Errorf("lsp service client evaluator error: %w", err)
	}
	sc.LSPServiceClientEvaluator = eval

	sc.Location = c.Location

	sc.TreeSitterLanguages = make(map[string]*tree_sitter.Language)
	sc.ExtMap = make(map[string]string)
	sc.NodeCache = make(map[string]*tree_sitter.Node)
	sc.LastModified = make(map[string]time.Time)

	sc.TreeSitterLanguages["java"] = tree_sitter.NewLanguage(tree_sitter_java.Language())
	sc.ExtMap["java"] = ".java"

	return sc, nil
}

func (g *TreeSitterServiceClientBuilder) GetGenericServiceClientCapabilities(log logr.Logger) []base.LSPServiceClientCapability {
	caps := []base.LSPServiceClientCapability{}
	r := openapi3.NewReflector()
	refCap, err := provider.ToProviderCap(r, log, base.ReferencedCondition{}, "referenced")
	if err != nil {
		log.Error(err, "unable to get referenced cap")
	} else {
		caps = append(caps, base.LSPServiceClientCapability{
			Capability: refCap,
			Fn:         serviceClientFn(base.EvaluateReferenced[*TreeSitterServiceClient]),
		})
	}

	depCap, err := provider.ToProviderCap(r, log, base.NoOpCondition{}, "dependency")
	if err != nil {
		log.Error(err, "unable to get referenced cap")
	} else {
		caps = append(caps, base.LSPServiceClientCapability{
			Capability: depCap,
			Fn:         serviceClientFn(base.EvaluateNoOp[*TreeSitterServiceClient]),
		})
	}

	echoCap, err := provider.ToProviderCap(r, log, echoCondition{}, "echo")
	if err != nil {
		log.Error(err, "unable to get echo cap")
	} else {
		caps = append(caps, base.LSPServiceClientCapability{
			Capability: echoCap,
			Fn:         serviceClientFn((*TreeSitterServiceClient).EvaluateEcho),
		})
	}

	queryCap, err := provider.ToProviderCap(r, log, queryCondition{}, "query")
	if err != nil {
		log.Error(err, "unable to get query cap")
	} else {
		caps = append(caps, base.LSPServiceClientCapability{
			Capability: queryCap,
			Fn:         serviceClientFn((*TreeSitterServiceClient).EvaluateQuery),
		})
	}

	return caps

}

// Example condition
type echoCondition struct {
	Echo struct {
		Input string `yaml:"input" json:"input"`
	} `yaml:"echo" json:"input"`
}

// Example evaluate
func (sc *TreeSitterServiceClient) EvaluateEcho(ctx context.Context, cap string, info []byte) (provider.ProviderEvaluateResponse, error) {
	var cond echoCondition
	err := yaml.Unmarshal(info, &cond)
	if err != nil {
		return provider.ProviderEvaluateResponse{}, fmt.Errorf("error unmarshaling query info")
	}

	return provider.ProviderEvaluateResponse{
		Matched: true,
		Incidents: []provider.IncidentContext{
			{
				FileURI: "file://test",
				Variables: map[string]interface{}{
					"output": cond.Echo.Input,
				},
			},
		},
	}, nil
}

type queryCondition struct {
	Query struct {
		Language string `yaml:"language" json:"language"`
		Query    string `yaml:"query" json:"query"`
	} `yaml:"query" json:"query"`
}

func (sc *TreeSitterServiceClient) EvaluateQuery(ctx context.Context, cap string, info []byte) (provider.ProviderEvaluateResponse, error) {
	var cond queryCondition
	err := yaml.Unmarshal(info, &cond)
	if err != nil {
		return provider.ProviderEvaluateResponse{}, fmt.Errorf("error unmarshaling query info: %w", err)
	}

	tsLanguage, ok := sc.TreeSitterLanguages[cond.Query.Language]
	if !ok {
		return provider.ProviderEvaluateResponse{}, fmt.Errorf("language not supported")
	}
	// Walk through all files in sc.Location recursively.
	// For each file, parse it with tree-sitter, run the query and return any matches.
	var incidents []provider.IncidentContext

	err = filepath.Walk(sc.Location, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Skip directories.
		if info.IsDir() {
			return nil
		}

		// doesn't end with .java
		ext, ok := sc.ExtMap[cond.Query.Language]
		if !ok {
			return nil
		}

		if filepath.Ext(path) != ext {
			return nil
		}

		// fileInfo, err := os.Stat(path)
		// if err != nil {
		// 	return err
		// }

		// modTime := fileInfo.ModTime()
		// if lastMod, ok := sc.LastModified[path]; !ok || !lastMod.Equal(modTime) {
		// 	sc.LastModified[path] = modTime
		// }

		// Read file content.
		src, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		// Create a new parser and set its language.
		parser := tree_sitter.NewParser()
		defer parser.Close()
		parser.SetLanguage(tsLanguage)

		// Parse the file content.
		tree := parser.Parse(src, nil)

		// Compile the query from cond.Query.
		query, queryErr := tree_sitter.NewQuery(tsLanguage, cond.Query.Query)
		if queryErr != nil {
			return err
		}
		defer query.Close()

		// Execute the query.
		cursor := tree_sitter.NewQueryCursor()
		defer cursor.Close()

		captures := cursor.Captures(query, tree.RootNode(), src)

		for match, index := captures.Next(); match != nil; match, index = captures.Next() {
			node := match.Captures[index].Node
			nodeStartPosition := node.StartPosition()
			nodeEndPosition := node.EndPosition()

			location := provider.Location{
				StartPosition: provider.Position{
					Line:      float64(nodeStartPosition.Row),
					Character: float64(nodeStartPosition.Column),
				},
				EndPosition: provider.Position{
					Line:      float64(nodeEndPosition.Row),
					Character: float64(nodeEndPosition.Column),
				},
			}

			incidents = append(incidents, provider.IncidentContext{
				FileURI:      uri.New("file://" + path),
				CodeLocation: &location,
				Variables:    map[string]interface{}{},
			})
		}

		return nil
	})
	if err != nil {
		return provider.ProviderEvaluateResponse{}, err
	}

	if len(incidents) > 0 {
		return provider.ProviderEvaluateResponse{
			Matched:   true,
			Incidents: incidents,
		}, nil
	}

	return provider.ProviderEvaluateResponse{}, nil
}
