package esxi

import "time"

const WindowNote = "standalone ESXi historicalInterval=0; realtime window ~1 hour at 20s"

var DefaultMetricNames = []string{
	"cpu.usagemhz.average",
	"cpu.usage.average",
	"mem.active.average",
	"net.bytesRx.average",
	"net.bytesTx.average",
	"net.usage.average",
}

// About is GET /v1/about.
type About struct {
	Product        string `json:"product"`
	APIVersion     string `json:"apiVersion"`
	Build          string `json:"build"`
	OSType         string `json:"osType"`
	Name           string `json:"name"`
	InstanceUUID   string `json:"instanceUuid,omitempty"`
	StandaloneESXi bool   `json:"standaloneEsxi"`
	Note           string `json:"note"`
}

type Question struct {
	ID      string            `json:"id"`
	Text    string            `json:"text"`
	Choices map[string]string `json:"choices,omitempty"`
}

type NIC struct {
	Label          string   `json:"label"`
	Key            int32    `json:"key"`
	MAC            string   `json:"mac,omitempty"`
	Portgroup      string   `json:"portgroup,omitempty"`
	Adapter        string   `json:"adapter,omitempty"`
	Connected      *bool    `json:"connected,omitempty"`
	StartConnected *bool    `json:"startConnected,omitempty"`
	AddressType    string   `json:"addressType,omitempty"`
	IPAddresses    []string `json:"ipAddresses,omitempty"`
}

type Disk struct {
	Label         string `json:"label"`
	Key           int32  `json:"key"`
	FileName      string `json:"fileName,omitempty"`
	Capacity      int64  `json:"capacityKB,omitempty"`
	Thin          *bool  `json:"thin,omitempty"`
	EagerlyScrub  *bool  `json:"eagerlyScrub,omitempty"`
	Mode          string `json:"mode,omitempty"`
	ControllerKey int32  `json:"controllerKey,omitempty"`
	UnitNumber    *int32 `json:"unitNumber,omitempty"`
	IOPSLimit     *int64 `json:"iopsLimit,omitempty"`
}

type VGPU struct {
	Label string `json:"label,omitempty"`
	Type  string `json:"type,omitempty"`
}

type QuickStats struct {
	OverallCPUUsage  int32 `json:"overallCpuUsage"`
	GuestMemoryUsage int32 `json:"guestMemoryUsage"`
	HostMemoryUsage  int32 `json:"hostMemoryUsage"`
	UptimeSeconds    int32 `json:"uptimeSeconds"`
	ConsumedOverhead int32 `json:"consumedOverhead,omitempty"`
	PrivateMemory    int32 `json:"privateMemory,omitempty"`
}

type GuestInfo struct {
	ToolsVersionText   string   `json:"toolsVersionText,omitempty"`
	ToolsVersion       string   `json:"toolsVersion,omitempty"`
	ToolsInstallType   string   `json:"toolsInstallType,omitempty"`
	ToolsRunningStatus string   `json:"toolsRunningStatus,omitempty"`
	IPAddresses        []string `json:"ipAddresses,omitempty"`
	HostName           string   `json:"hostName,omitempty"`
	IPAddress          string   `json:"ipAddress,omitempty"`
	ToolsStatus        string   `json:"toolsStatus,omitempty"`
	GuestUnavailable   bool     `json:"guestUnavailable"`
}

type VMDeviceInfo struct {
	Label      string `json:"label"`
	Type       string `json:"type"`
	Key        int32  `json:"key"`
	Displays   int32  `json:"displays,omitempty"`
	VideoRAMKB int64  `json:"videoRAMKB,omitempty"`
	Enable3D   *bool  `json:"enable3D,omitempty"`
}

type VMConfigState struct {
	DeviceEditable      bool           `json:"deviceEditable,omitempty"`
	Editable            bool           `json:"editable,omitempty"`
	ChangeVersion       string         `json:"changeVersion,omitempty"`
	Version             string         `json:"version,omitempty"`
	CoresPerSocket      *int32         `json:"coresPerSocket,omitempty"`
	Annotation          string         `json:"annotation"`
	BootDelayMS         *int64         `json:"bootDelayMS,omitempty"`
	BootRetry           *bool          `json:"bootRetry,omitempty"`
	ToolsUpgradePolicy  string         `json:"toolsUpgradePolicy,omitempty"`
	StandbyAction       string         `json:"standbyAction,omitempty"`
	Firmware            string         `json:"firmware,omitempty"`
	VMX                 string         `json:"vmx,omitempty"`
	SuspendDirectory    string         `json:"suspendDirectory,omitempty"`
	CPUReservationMHz   *int64         `json:"cpuReservationMHz,omitempty"`
	CPULimitMHz         *int64         `json:"cpuLimitMHz,omitempty"`
	MemoryReservationMB *int64         `json:"memoryReservationMB,omitempty"`
	MemoryLimitMB       *int64         `json:"memoryLimitMB,omitempty"`
	CPUShares           string         `json:"cpuShares,omitempty"`
	MemoryShares        string         `json:"memoryShares,omitempty"`
	MemoryLocked        *bool          `json:"memoryLocked,omitempty"`
	VPMC                *bool          `json:"vpmc,omitempty"`
	VBS                 *bool          `json:"vbs,omitempty"`
	SecureBoot          *bool          `json:"secureBoot,omitempty"`
	MaxMksConnections   int32          `json:"maxMksConnections,omitempty"`
	GuestAutoLock       *bool          `json:"guestAutoLock,omitempty"`
	Devices             []VMDeviceInfo `json:"devices,omitempty"`
	NestedHV            *bool          `json:"nestedHV,omitempty"`
	IOMMU               *bool          `json:"iommu,omitempty"`
	CPUHotAdd           *bool          `json:"cpuHotAddEnabled,omitempty"`
	MemoryHotAdd        *bool          `json:"memoryHotAddEnabled,omitempty"`
}

type VMInfo struct {
	ID                 string         `json:"id"`
	UUID               string         `json:"uuid,omitempty"`
	MoRef              string         `json:"moref"`
	Name               string         `json:"name"`
	InstanceUUID       string         `json:"instanceUuid,omitempty"`
	BiosUUID           string         `json:"biosUuid,omitempty"`
	VBSEnabled         *bool          `json:"vbsEnabled,omitempty"`
	GuestOS            string         `json:"guestOS,omitempty"`
	GuestID            string         `json:"guestId,omitempty"`
	PowerState         string         `json:"powerState"`
	ConnectionState    string         `json:"connectionState,omitempty"`
	Config             *VMConfigState `json:"config,omitempty"`
	Question           *Question      `json:"question,omitempty"`
	NumCPU             int32          `json:"numCPU"`
	MemoryMB           int32          `json:"memoryMB"`
	NICs               []NIC          `json:"networks,omitempty"`
	Disks              []Disk         `json:"disks,omitempty"`
	VGPU               []VGPU         `json:"vgpu,omitempty"`
	CommittedBytes     *int64         `json:"committedBytes,omitempty"`
	UncommittedBytes   *int64         `json:"uncommittedBytes,omitempty"`
	UnsharedBytes      *int64         `json:"unsharedBytes,omitempty"`
	QuickStats         *QuickStats    `json:"quickStats,omitempty"`
	QuickStatsReliable bool           `json:"quickStatsReliable"`
	Guest              GuestInfo      `json:"guest"`
	CPUPct             *float64       `json:"cpuPct,omitempty"`
	MemActivePct       *float64       `json:"memActivePct,omitempty"`
	NetRxMbps          *float64       `json:"netRxMbps,omitempty"`
	NetTxMbps          *float64       `json:"netTxMbps,omitempty"`
}

type HostInfo struct {
	UptimeSeconds   *int64      `json:"uptimeSeconds,omitempty"`
	OverallStatus   string      `json:"overallStatus,omitempty"`
	ConnectionState string      `json:"connectionState,omitempty"`
	MaintenanceMode bool        `json:"maintenanceMode,omitempty"`
	Name            string      `json:"name"`
	MoRef           string      `json:"moref"`
	CPUMhz          int32       `json:"cpuMhz"`
	NumCPU          int16       `json:"numCpu"`
	NumCpuCores     int16       `json:"numCpuCores,omitempty"`
	NumCpuThreads   int16       `json:"numCpuThreads,omitempty"`
	NumCpuPkgs      int16       `json:"numCpuPkgs,omitempty"`
	MemoryMB        int64       `json:"memoryMB"`
	Portgroups      []Portgroup `json:"portgroups,omitempty"`
}

type Portgroup struct {
	Name    string `json:"name"`
	VLAN    int32  `json:"vlanId,omitempty"`
	VSwitch string `json:"vswitch,omitempty"`
}

type NetworkInfo struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

type DatastoreInfo struct {
	Name                   string `json:"name"`
	Type                   string `json:"type,omitempty"`
	Capacity               int64  `json:"capacity,omitempty"`
	FreeSpace              int64  `json:"freeSpace"`
	URL                    string `json:"url,omitempty"`
	Accessible             *bool  `json:"accessible,omitempty"`
	MaintenanceMode        string `json:"maintenanceMode,omitempty"`
	Status                 string `json:"status,omitempty"`
	VMCount                int    `json:"vmCount"`
	Uncommitted            *int64 `json:"uncommitted,omitempty"`
	MaxFileSize            int64  `json:"maxFileSize,omitempty"`
	MaxVirtualDiskCapacity int64  `json:"maxVirtualDiskCapacity,omitempty"`
	UpdatedAt              string `json:"updatedAt,omitempty"`
	VMFSVersion            string `json:"vmfsVersion,omitempty"`
}

type DSFile struct {
	Path     string `json:"path"`
	Folder   bool   `json:"folder"`
	Size     int64  `json:"size,omitempty"`
	Modified string `json:"modified,omitempty"`
}

type MetricSample struct {
	TimeUTC time.Time `json:"timeUtc"`
	Value   float64   `json:"value"`
}

type MetricSeriesOut struct {
	Name     string         `json:"name"`
	Instance string         `json:"instance"`
	Unit     string         `json:"unit"`
	RawUnit  string         `json:"rawUnit,omitempty"`
	Points   []MetricSample `json:"points"`
	Derived  map[string]any `json:"derived,omitempty"`
}

type MetricsResponse struct {
	Entity     string            `json:"entity"`
	WindowNote string            `json:"windowNote"`
	Units      map[string]string `json:"units"`
	Series     []MetricSeriesOut `json:"series"`
}

type CounterInfo struct {
	Name        string `json:"name"`
	Group       string `json:"group,omitempty"`
	Unit        string `json:"unit,omitempty"`
	Rollup      string `json:"rollup,omitempty"`
	Description string `json:"description,omitempty"`
}

type CloneLayout struct {
	Folder string `json:"folder"`
	Prefix string `json:"prefix"`
}

type ClonePlan struct {
	SourceDatastore      string       `json:"sourceDatastore,omitempty"`
	DestinationDatastore string       `json:"dstDatastore,omitempty"`
	Configuration        *CloneSpec   `json:"configuration,omitempty"`
	DryRun               bool         `json:"dryRun"`
	Note                 string       `json:"note"`
	Layout               *CloneLayout `json:"layout,omitempty"`
	Steps                []CloneStep  `json:"steps"`
}

type CloneStep struct {
	Op                   string `json:"op"`
	Summary              string `json:"summary"`
	Datastore            string `json:"datastore,omitempty"`
	DestinationDatastore string `json:"dstDatastore,omitempty"`
	Src                  string `json:"src,omitempty"`
	Dst                  string `json:"dst,omitempty"`
	Path                 string `json:"path,omitempty"`
	Name                 string `json:"name,omitempty"`
	Portgroup            string `json:"portgroup,omitempty"`
	NumCPU               int32  `json:"numCPU,omitempty"`
	MemoryMB             int32  `json:"memoryMB,omitempty"`
	NestedHV             *bool  `json:"nestedHV,omitempty"`
	Choice               string `json:"choice,omitempty"`
}

type StepResult struct {
	Op      string `json:"op"`
	DryRun  bool   `json:"dryRun"`
	Message string `json:"message"`
	Detail  any    `json:"detail,omitempty"`
}

type CloneSpec struct {
	SourceVersion        string          `json:"sourceVersion,omitempty"`
	CPUHotAdd            *bool           `json:"cpuHotAddEnabled,omitempty"`
	MemoryHotAdd         *bool           `json:"memoryHotAddEnabled,omitempty"`
	MemoryReservationMB  *int64          `json:"memoryReservationMB,omitempty"`
	ExpandDisks          []DiskExpansion `json:"expandDisks,omitempty"`
	Src                  string          `json:"src"`
	Dst                  string          `json:"dst"`
	Datastore            string          `json:"datastore"`
	DestinationDatastore string          `json:"dstDatastore,omitempty"`
	Confirm              bool            `json:"confirm"`
	Portgroup            string          `json:"portgroup,omitempty"`
	NicAdapter           string          `json:"nicAdapter,omitempty"`
	ReplaceNic           *bool           `json:"replaceNic,omitempty"`
	NumCPU               *int32          `json:"numCPU,omitempty"`
	MemoryMB             *int32          `json:"memoryMB,omitempty"`
	LockMemory           *bool           `json:"lockMemory,omitempty"`
	NestedHV             *bool           `json:"nestedHV,omitempty"`
	IOMMU                *bool           `json:"iommu,omitempty"`
	PowerOn              *bool           `json:"powerOn,omitempty"`
	AnswerCopied         *bool           `json:"answerCopied,omitempty"`
	AnswerChoice         string          `json:"answerChoice,omitempty"`
	SrcFolder            string          `json:"srcFolder,omitempty"`
	SrcPrefix            string          `json:"srcPrefix,omitempty"`
	Async                *bool           `json:"async,omitempty"`
	DryRun               *bool           `json:"dryRun,omitempty"`
}

type CloneTaskStep struct {
	Op        string `json:"op"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
	StartedAt string `json:"startedAt,omitempty"`
	EndedAt   string `json:"endedAt,omitempty"`
}

type CloneTaskView struct {
	TaskID   string          `json:"taskId"`
	Status   string          `json:"status"`
	Op       string          `json:"op,omitempty"`
	Progress CloneProgress   `json:"progress"`
	Steps    []CloneTaskStep `json:"steps"`
	Layout   *CloneLayout    `json:"layout,omitempty"`
	Result   *VMInfo         `json:"result,omitempty"`
	Error    *CloneTaskError `json:"error,omitempty"`
	Poll     string          `json:"poll,omitempty"`
}

type CloneProgress struct {
	Percent int `json:"percent"`
	Step    int `json:"step"`
	Steps   int `json:"steps"`
}

type CloneTaskError struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	VimFault string `json:"vimFault,omitempty"`
	Op       string `json:"op,omitempty"`
}
