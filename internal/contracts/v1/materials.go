package v1

import (
	"encoding/json"
	"personaldesk/internal/application/access"
	"personaldesk/internal/application/materials"
	"personaldesk/internal/contracts"
	"strconv"
)

type UploadMaterial struct {
	ExpectedTaskRevision int64   `json:"expectedTaskRevision"`
	MediaType            string  `json:"mediaType"`
	Text                 string  `json:"text"`
	Label                *string `json:"label"`
}

// encoding/json replaces unpaired UTF-16 escapes with U+FFFD. New uploads must
// preserve the caller's exact Unicode text, so reject that lossy normalization.
// Existing v1 decoders and schema resources retain their published behavior.
func (u *UploadMaterial) UnmarshalJSON(data []byte) error {
	for i := 0; i < len(data); i++ {
		if data[i] != '\\' {
			continue
		}
		i++
		if i >= len(data) {
			return contracts.ErrInvalid
		}
		if data[i] != 'u' {
			continue
		}
		if i+4 >= len(data) {
			return contracts.ErrInvalid
		}
		value, err := strconv.ParseUint(string(data[i+1:i+5]), 16, 16)
		if err != nil {
			return contracts.ErrInvalid
		}
		i += 4
		if value >= 0xdc00 && value <= 0xdfff {
			return contracts.ErrInvalid
		}
		if value >= 0xd800 && value <= 0xdbff {
			if i+6 >= len(data) || data[i+1] != '\\' || data[i+2] != 'u' {
				return contracts.ErrInvalid
			}
			low, err := strconv.ParseUint(string(data[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return contracts.ErrInvalid
			}
			i += 6
		}
	}
	type plain UploadMaterial
	var value plain
	if err := json.Unmarshal(data, &value); err != nil {
		return contracts.ErrInvalid
	}
	*u = UploadMaterial(value)
	return nil
}

func (u UploadMaterial) Input(p access.Principal) materials.Upload {
	return materials.Upload{ExpectedTaskRevision: u.ExpectedTaskRevision, MediaType: u.MediaType, Text: u.Text, Label: u.Label, AppliedBy: p}
}

type UploadMaterialResult struct {
	WorkspaceRevision int64   `json:"workspaceRevision"`
	TaskRef           TaskRef `json:"taskRef"`
	MaterialID        string  `json:"materialId"`
}

func FromUploadMaterialResult(r materials.Result) UploadMaterialResult {
	return UploadMaterialResult{r.WorkspaceRevision, TaskRef{r.TaskRef.TaskID, r.TaskRef.Revision}, r.MaterialID}
}
