// Code generated by gen.py. DO NOT EDIT.

package expconf

import (
	"github.com/santhosh-tekuri/jsonschema/v2"

	"github.com/determined-ai/determined/master/pkg/schemas"
)

func (e EnvironmentImageMapV0) CPU() string {
	if e.RawCPU == nil {
		panic("You must call WithDefaults on EnvironmentImageMapV0 before .CPU")
	}
	return *e.RawCPU
}

func (e *EnvironmentImageMapV0) SetCPU(val string) {
	e.RawCPU = &val
}

func (e EnvironmentImageMapV0) CUDA() string {
	if e.RawCUDA == nil {
		panic("You must call WithDefaults on EnvironmentImageMapV0 before .CUDA")
	}
	return *e.RawCUDA
}

func (e *EnvironmentImageMapV0) SetCUDA(val string) {
	e.RawCUDA = &val
}

func (e EnvironmentImageMapV0) GPU() *string {
	return e.RawGPU
}

func (e *EnvironmentImageMapV0) SetGPU(val *string) {
	e.RawGPU = val
}

func (e EnvironmentImageMapV0) ROCM() string {
	if e.RawROCM == nil {
		panic("You must call WithDefaults on EnvironmentImageMapV0 before .ROCM")
	}
	return *e.RawROCM
}

func (e *EnvironmentImageMapV0) SetROCM(val string) {
	e.RawROCM = &val
}

func (e EnvironmentImageMapV0) ParsedSchema() interface{} {
	return schemas.ParsedEnvironmentImageMapV0()
}

func (e EnvironmentImageMapV0) SanityValidator() *jsonschema.Schema {
	return schemas.GetSanityValidator("http://determined.ai/schemas/expconf/v0/environment-image-map.json")
}

func (e EnvironmentImageMapV0) CompletenessValidator() *jsonschema.Schema {
	return schemas.GetCompletenessValidator("http://determined.ai/schemas/expconf/v0/environment-image-map.json")
}
