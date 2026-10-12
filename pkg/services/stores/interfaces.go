package stores

import "github.com/liut/morign/pkg/models/aigc"

// Storage is the storage interface defining access methods for various data stores
type Storage interface {
	Preset() aigc.Preset
	Corpus() CorpuStore // gened
	KB() CorpuStore     // alias
	MCP() MCPStore      // gened
	Convo() ConvoStore  // gened
	State() StateStore
	Verifier() VerifierStore
	Capability() CapabilityStore // gened
	Skill() SkillStore           // gened
}
