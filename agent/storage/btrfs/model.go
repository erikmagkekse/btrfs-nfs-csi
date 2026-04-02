package btrfs

type SubvolumeInfo struct {
	Path string
}

type BTRFSDevice struct {
	DevID          string
	Device         string
	Missing        bool
	SizeBytes      uint64
	AllocatedBytes uint64
	Errors         DeviceErrors
}

func (d BTRFSDevice) HasErrors() bool {
	return d.Errors.ReadErrs > 0 || d.Errors.WriteErrs > 0 || d.Errors.FlushErrs > 0 || d.Errors.CorruptionErrs > 0 || d.Errors.GenerationErrs > 0
}

type DeviceErrors struct {
	Device         string
	ReadErrs       uint64
	WriteErrs      uint64
	FlushErrs      uint64
	CorruptionErrs uint64
	GenerationErrs uint64
}

type FilesystemUsage struct {
	TotalBytes         uint64
	UnallocatedBytes   uint64
	UsedBytes          uint64
	FreeBytes          uint64
	DataRatio          float64
	MetadataUsedBytes  uint64
	MetadataTotalBytes uint64
}

type QgroupInfo struct {
	Referenced uint64
	Exclusive  uint64
}

type ScrubStatus struct {
	DataBytesScrubbed uint64 `json:"data_bytes_scrubbed"`
	TreeBytesScrubbed uint64 `json:"tree_bytes_scrubbed"`
	ReadErrors        uint64 `json:"read_errors"`
	CSumErrors        uint64 `json:"csum_errors"`
	VerifyErrors      uint64 `json:"verify_errors"`
	SuperErrors       uint64 `json:"super_errors"`
	UncorrectableErrs uint64 `json:"uncorrectable_errors"`
	CorrectedErrs     uint64 `json:"corrected_errors"`
	Running           bool   `json:"running"`
}
