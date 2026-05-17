package launch

import (
	"fmt"

	"github.com/hollis-labs/go-agent-launch/agentlaunch"
)

// Input is one typed boot input surfaced by `describe`. The shape is the
// FROZEN contract's input row: name/type/required/default/description.
type Input struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Default     any    `json:"default"`
	Description string `json:"description"`
}

// Description is the `describe` result: a launchable entry's typed
// inputs plus the available runners.
type Description struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Inputs  []Input  `json:"inputs"`
	Runners []string `json:"runners"`
}

// Describe resolves the typed BootInputs of the LaunchSpec a given launch
// bag invokes, so the Swift app can render an input form. The id is a
// launch-bag id from `list`.
func Describe(cat *Catalog, id string) (Description, error) {
	bag, ok := cat.Bag(id)
	if !ok {
		return Description{}, fmt.Errorf("unknown spec %q", id)
	}

	display := inputString(cat.Spec, bag, "display_name")
	if display == "" {
		display = bag.Name
	}

	inputs := make([]Input, 0, len(cat.Spec.Inputs))
	for i := range cat.Spec.Inputs {
		in := cat.Spec.Inputs[i]
		inputs = append(inputs, Input{
			Name:        in.Name,
			Type:        in.Type,
			Required:    in.Required,
			Default:     bagOrSpecDefault(bag, in),
			Description: in.Description,
		})
	}
	return Description{
		ID:      bag.Name,
		Name:    display,
		Inputs:  inputs,
		Runners: cat.KnownRunners(),
	}, nil
}

// bagOrSpecDefault returns the effective default for an input field in
// the describe form: the bag's concrete value pre-fills the field when
// present, otherwise the LaunchSpec's declared default.
func bagOrSpecDefault(bag agentlaunch.LaunchBag, in agentlaunch.BootInput) any {
	if v, ok := bag.Inputs[in.Name]; ok {
		return v
	}
	return in.Default
}
