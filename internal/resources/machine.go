// Package resources contains resources stored in the TrueNAS infra provider state.
package resources

import (
	"github.com/cosi-project/runtime/pkg/resource"
	"github.com/cosi-project/runtime/pkg/resource/meta"
	"github.com/cosi-project/runtime/pkg/resource/protobuf"
	"github.com/cosi-project/runtime/pkg/resource/typed"
	"github.com/siderolabs/omni/client/pkg/infra"

	"github.com/zclifton/omni-infra-provider-truenas/api/specs"
	providermeta "github.com/zclifton/omni-infra-provider-truenas/internal/resources/meta"
)

// NewMachine creates new Machine.
func NewMachine(ns, id string) *Machine {
	return typed.NewResource[MachineSpec, MachineExtension](
		resource.NewMetadata(ns, infra.ResourceType("Machine", providermeta.ProviderID), id, resource.VersionUndefined),
		protobuf.NewResourceSpec(&specs.MachineSpec{}),
	)
}

// Machine describes TrueNAS machine configuration.
type Machine = typed.Resource[MachineSpec, MachineExtension]

// MachineSpec wraps specs.MachineSpec.
type MachineSpec = protobuf.ResourceSpec[specs.MachineSpec, *specs.MachineSpec]

// MachineExtension provides auxiliary methods for Machine resource.
type MachineExtension struct{}

// ResourceDefinition implements [typed.Extension] interface.
func (MachineExtension) ResourceDefinition() meta.ResourceDefinitionSpec {
	return meta.ResourceDefinitionSpec{
		Type:             infra.ResourceType("Machine", providermeta.ProviderID),
		Aliases:          []resource.Type{},
		DefaultNamespace: infra.ResourceNamespace(providermeta.ProviderID),
		PrintColumns:     []meta.PrintColumn{},
	}
}
