package lvm

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/cockroachdb/errors"

	lhexec "github.com/longhorn/go-common-libs/exec"
	lhtypes "github.com/longhorn/go-common-libs/types"
)

const (
	lvmLogicalVolumeFields = "vg_name,lv_name,lv_size,lv_active,lv_path,lv_tags,lv_attr,pool_lv,data_percent"
	maxLVMNameLen          = 127
)

// isValidLVMName reports whether name is a valid LVM volume group or logical
// volume name, after the "VALID NAMES" section of lvm(8): 1..127 bytes of
// [a-zA-Z0-9+_.-], neither "." nor "..", and not starting with '-'. Validating
// every name that enters a --select expression or an argument also keeps a
// crafted name from changing what the expression matches.
func isValidLVMName(name string) bool {
	if len(name) == 0 || len(name) > maxLVMNameLen || name == "." || name == ".." || name[0] == '-' {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		isAlphaNum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !isAlphaNum && c != '+' && c != '_' && c != '.' && c != '-' {
			return false
		}
	}
	return true
}

// isValidLVMUUID reports whether id is a valid LVM UUID token: an LVM UUID is
// 32 characters of lvm2's base-62 alphabet ([a-zA-Z0-9], lib/uuid/uuid.c),
// reported in dash-separated groups, so [a-zA-Z0-9-] not starting with '-'.
func isValidLVMUUID(id string) bool {
	if len(id) == 0 || len(id) > maxLVMNameLen || id[0] == '-' {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		isAlphaNum := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
		if !isAlphaNum && c != '-' {
			return false
		}
	}
	return true
}

// LogicalVolume is the LVM metadata used by local-engine instance and
// metrics operations.
type LogicalVolume struct {
	VGName string
	Name   string
	Size   int64
	Active bool
	Path   string
	Tags   []string
	Attr   string
	PoolLV string
	// DataUsagePercentage is set for thin LVs and thin pools.
	DataUsagePercentage float64
}

type lvmLogicalVolumeReport struct {
	Reports []struct {
		LVs []lvmLogicalVolumeEntry `json:"lv"`
	} `json:"report"`
}

type lvmLogicalVolumeEntry struct {
	VGName      string `json:"vg_name"`
	LVName      string `json:"lv_name"`
	LVSize      string `json:"lv_size"`
	LVActive    string `json:"lv_active"`
	LVPath      string `json:"lv_path"`
	LVTags      string `json:"lv_tags"`
	LVAttr      string `json:"lv_attr"`
	PoolLV      string `json:"pool_lv"`
	DataPercent string `json:"data_percent"`
}

func (lv LogicalVolume) IsThinPool() bool {
	return lv.Name == ThinPoolName && len(lv.Attr) > 0 && lv.Attr[0] == 't'
}

func (lv LogicalVolume) IsThinVolume() bool {
	return lv.PoolLV == ThinPoolName
}

// ListLogicalVolumes returns all LVs in volume groups managed by Longhorn.
func ListLogicalVolumes(executor lhexec.ExecuteInterface) ([]LogicalVolume, error) {
	return queryLogicalVolumes(executor, "vg_tags="+VGTag)
}

// listVGLogicalVolumes returns the LVs of one volume group.
func listVGLogicalVolumes(executor lhexec.ExecuteInterface, vgName string) ([]LogicalVolume, error) {
	if !isValidLVMName(vgName) {
		return nil, errors.Errorf("invalid volume group name %q", vgName)
	}
	return queryLogicalVolumes(executor, "vg_name="+vgName)
}

// GetLogicalVolume returns one LV from a Longhorn-managed VG, or nil when
// it does not exist.
func GetLogicalVolume(executor lhexec.ExecuteInterface, vgName, lvName string) (*LogicalVolume, error) {
	if !isValidLVMName(vgName) || !isValidLVMName(lvName) {
		return nil, errors.Errorf("invalid logical volume name %q/%q", vgName, lvName)
	}
	selection := fmt.Sprintf("vg_name=%s && lv_name=%s", vgName, lvName)
	lvs, err := queryLogicalVolumes(executor, selection)
	if err != nil {
		return nil, err
	}
	if len(lvs) == 0 {
		return nil, nil
	}
	if len(lvs) != 1 {
		return nil, errors.Errorf("expected one logical volume %v/%v, found %v", vgName, lvName, len(lvs))
	}
	return &lvs[0], nil
}

func queryLogicalVolumes(executor lhexec.ExecuteInterface, selection string) ([]LogicalVolume, error) {
	args := []string{"--reportformat", "json", "--units", "b", "--nosuffix"}
	if selection != "" {
		args = append(args, "--select", selection)
	}
	args = append(args, "-o", lvmLogicalVolumeFields)
	output, err := executor.Execute(nil, "lvs", CommandArgs(args...), lhtypes.ExecuteDefaultTimeout)
	if err != nil {
		return nil, errors.Wrapf(err, "failed to query logical volumes")
	}
	return parseLogicalVolumes(output)
}

func parseLogicalVolumes(output string) ([]LogicalVolume, error) {
	var report lvmLogicalVolumeReport
	if err := json.Unmarshal([]byte(output), &report); err != nil {
		return nil, errors.Wrapf(err, "failed to parse lvs JSON output")
	}

	var lvs []LogicalVolume
	for _, section := range report.Reports {
		for _, entry := range section.LVs {
			size, err := strconv.ParseInt(strings.TrimSpace(entry.LVSize), 10, 64)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to parse size of logical volume %v/%v", entry.VGName, entry.LVName)
			}
			dataUsage, err := parsePercentage(entry.DataPercent)
			if err != nil {
				return nil, errors.Wrapf(err, "failed to parse data usage of logical volume %v/%v", entry.VGName, entry.LVName)
			}
			lvs = append(lvs, LogicalVolume{
				VGName:              entry.VGName,
				Name:                entry.LVName,
				Size:                size,
				Active:              strings.TrimSpace(entry.LVActive) == "active",
				Path:                strings.TrimSpace(entry.LVPath),
				Tags:                parseLVMTags(entry.LVTags),
				Attr:                strings.TrimSpace(entry.LVAttr),
				PoolLV:              strings.TrimSpace(entry.PoolLV),
				DataUsagePercentage: dataUsage,
			})
		}
	}
	return lvs, nil
}

func parsePercentage(value string) (float64, error) {
	value = strings.TrimSpace(value)
	if value == "" || value == "-" {
		return 0, nil
	}
	return strconv.ParseFloat(value, 64)
}

// parseLVMTags splits the comma-separated tag list of an LVM report field.
func parseLVMTags(field string) []string {
	var tags []string
	for _, tag := range strings.Split(field, ",") {
		if tag = strings.TrimSpace(tag); tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}
