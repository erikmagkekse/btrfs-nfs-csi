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
