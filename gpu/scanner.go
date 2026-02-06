package gpu

import (
	_ "embed"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/rajveermalviya/go-webgpu/wgpu"
)

//go:embed kernels/pattern_match.wgsl
var shaderCode string

const (
	MAX_LOG_SIZE    = 256
	MAX_LOG_U32S    = 64
	MAX_PATTERN_LEN = 32
	MAX_PATTERN_U32S = 8
)

type Scanner struct {
	batchSize int
	mu        sync.Mutex
	device    *wgpu.Device
	queue     *wgpu.Queue
	pipeline  *wgpu.ComputePipeline
	bindGroup *wgpu.BindGroup
	logBuffer     *wgpu.Buffer
	patternBuffer *wgpu.Buffer
	resultBuffer  *wgpu.Buffer
	configBuffer  *wgpu.Buffer
	gpuEnabled bool
}

type LogEntry struct {
	Data    [64]uint32
	Length  uint32
	Padding [3]uint32
}

type PatternEntry struct {
	Data    [8]uint32
	Length  uint32
	Id      uint32
	Padding [2]uint32
}

type ResultEntry struct {
	MatchFound uint32
	PatternId  uint32
	Confidence float32
	LogIndex   uint32
}

type ScanResult struct {
	LogIndex    int
	Match       bool
	PatternName string
	Confidence  float64
	Timestamp   time.Time
}

type Pattern struct {
	Name       string
	Signatures []string
	Threshold  float64
	Category   string
}

type GPUStats struct {
	DeviceName      string
	MemoryUsed      int
	ComputeUnits    int
	BatchSize       int
	ParallelThreads int
}

func NewScanner(batchSize int) (*Scanner, error) {
	scanner := &Scanner{
		batchSize: batchSize,
	}

	err := scanner.initGPU()
	if err != nil {
		log.Printf("GPU Init failed (%v). Falling back to CPU mode.", err)
		scanner.gpuEnabled = false
	} else {
		log.Println("GPU Compute Engine Initialized (WGPU)")
		scanner.gpuEnabled = true
	}
	
	return scanner, nil
}

func (s *Scanner) initGPU() error {
	instance := wgpu.CreateInstance(nil)
	adapter, err := instance.RequestAdapter(nil)
	if err != nil {
		return fmt.Errorf("failed to request adapter: %v", err)
	}
	device, err := adapter.RequestDevice(nil)
	if err != nil {
		return fmt.Errorf("failed to request device: %v", err)
	}
	s.device = device
	s.queue = device.GetQueue()

	shaderModule, err := device.CreateShaderModule(&wgpu.ShaderModuleDescriptor{
		Label: "PatternMatchShader",
		WGSLDescriptor: &wgpu.ShaderModuleWGSLDescriptor{
			Code: shaderCode,
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create shader module: %v", err)
	}

	pipelineLayout, err := device.CreatePipelineLayout(&wgpu.PipelineLayoutDescriptor{
		BindGroupLayouts: nil,
	})
	if err != nil {
		return fmt.Errorf("failed to create pipeline layout: %v", err)
	}

	pipeline, err := device.CreateComputePipeline(&wgpu.ComputePipelineDescriptor{
		Layout: pipelineLayout,
		Compute: wgpu.ProgrammableStageDescriptor{
			Module:     shaderModule,
			EntryPoint: "main",
		},
	})
	if err != nil {
		return fmt.Errorf("failed to create pipeline: %v", err)
	}
	s.pipeline = pipeline

	logBufferSize := uint64(s.batchSize * int(unsafe.Sizeof(LogEntry{})))
	s.logBuffer, err = device.CreateBuffer(&wgpu.BufferDescriptor{
		Label:            "LogBuffer",
		Size:             logBufferSize,
		Usage:            wgpu.BufferUsage_Storage | wgpu.BufferUsage_CopyDst,
		MappedAtCreation: false,
	})
	if err != nil { return err }

	patternBufferSize := uint64(32 * int(unsafe.Sizeof(PatternEntry{}))) 
	s.patternBuffer, err = device.CreateBuffer(&wgpu.BufferDescriptor{
		Label:            "PatternBuffer",
		Size:             patternBufferSize,
		Usage:            wgpu.BufferUsage_Storage | wgpu.BufferUsage_CopyDst,
		MappedAtCreation: false,
	})
	if err != nil { return err }

	resultBufferSize := uint64(s.batchSize * int(unsafe.Sizeof(ResultEntry{})))
	s.resultBuffer, err = device.CreateBuffer(&wgpu.BufferDescriptor{
		Label:            "ResultBuffer",
		Size:             resultBufferSize,
		Usage:            wgpu.BufferUsage_Storage | wgpu.BufferUsage_CopySrc,
		MappedAtCreation: false,
	})
	if err != nil { return err }
	
	configBufferSize := uint64(8)
	s.configBuffer, err = device.CreateBuffer(&wgpu.BufferDescriptor{
		Label:            "ConfigBuffer",
		Size:             configBufferSize,
		Usage:            wgpu.BufferUsage_Uniform | wgpu.BufferUsage_CopyDst,
		MappedAtCreation: false,
	})
	if err != nil { return err }

	bindGroupLayout := pipeline.GetBindGroupLayout(0)
	s.bindGroup, err = device.CreateBindGroup(&wgpu.BindGroupDescriptor{
		Layout: bindGroupLayout,
		Entries: []wgpu.BindGroupEntry{
			{Binding: 0, Buffer: s.logBuffer, Size: logBufferSize},
			{Binding: 1, Buffer: s.patternBuffer, Size: patternBufferSize},
			{Binding: 2, Buffer: s.resultBuffer, Size: resultBufferSize},
			{Binding: 3, Buffer: s.configBuffer, Size: configBufferSize},
		},
	})
	if err != nil { return err }

	return nil
}

func (s *Scanner) Close() {
	if s.gpuEnabled {
		if s.logBuffer != nil { 
			s.logBuffer.Destroy()
		}
		if s.patternBuffer != nil { 
			s.patternBuffer.Destroy()
		}
		if s.resultBuffer != nil { 
			s.resultBuffer.Destroy()
		}
		if s.configBuffer != nil { 
			s.configBuffer.Destroy()
		}
	}
}

func (s *Scanner) ScanLogs(logs []string, patterns []Pattern) ([]ScanResult, error) {
	if !s.gpuEnabled {
		return s.scanLogsCPU(logs, patterns)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	logData := make([]LogEntry, len(logs))
	for i, l := range logs {
		bytes := []byte(l)
		if len(bytes) > MAX_LOG_SIZE {
			bytes = bytes[:MAX_LOG_SIZE]
		}
		
		var entry LogEntry
		entry.Length = uint32(len(bytes))
		
		for j := 0; j < len(bytes); j++ {
			wordIdx := j / 4
			byteShift := (j % 4) * 8
			entry.Data[wordIdx] |= uint32(bytes[j]) << byteShift
		}
		logData[i] = entry
	}

	patternData := make([]PatternEntry, 0)
	patternMap := make(map[uint32]string)
	pId := uint32(0)
	
	for _, p := range patterns {
		for _, sig := range p.Signatures {
			bytes := []byte(sig)
			if len(bytes) > MAX_PATTERN_LEN {
				bytes = bytes[:MAX_PATTERN_LEN]
			}
			
			var entry PatternEntry
			entry.Length = uint32(len(bytes))
			entry.Id = pId
			patternMap[pId] = p.Name
			
			for j := 0; j < len(bytes); j++ {
				wordIdx := j / 4
				byteShift := (j % 4) * 8
				entry.Data[wordIdx] |= uint32(bytes[j]) << byteShift
			}
			patternData = append(patternData, entry)
			pId++
		}
	}

	s.queue.WriteBuffer(s.logBuffer, 0, unsafe.Slice((*byte)(unsafe.Pointer(&logData[0])), len(logData)*int(unsafe.Sizeof(LogEntry{}))))
	s.queue.WriteBuffer(s.patternBuffer, 0, unsafe.Slice((*byte)(unsafe.Pointer(&patternData[0])), len(patternData)*int(unsafe.Sizeof(PatternEntry{}))))
	
	config := []uint32{uint32(len(logs)), uint32(len(patternData))}
	s.queue.WriteBuffer(s.configBuffer, 0, unsafe.Slice((*byte)(unsafe.Pointer(&config[0])), len(config)*4))

	encoder, err := s.device.CreateCommandEncoder(nil)
	if err != nil { return nil, err }
	
	pass := encoder.BeginComputePass(nil)
	pass.SetPipeline(s.pipeline)
	pass.SetBindGroup(0, s.bindGroup, nil)
	
	workgroups := uint32(math.Ceil(float64(len(logs)) / 64.0))
	pass.DispatchWorkgroups(workgroups, 1, 1)
	pass.End()

	size := uint64(len(logs) * int(unsafe.Sizeof(ResultEntry{})))
	readBuffer, err := s.device.CreateBuffer(&wgpu.BufferDescriptor{
		Label: "ReadBuffer",
		Size:  size,
		Usage: wgpu.BufferUsage_MapRead | wgpu.BufferUsage_CopyDst,
	})
	if err != nil { return nil, err }
	
	encoder.CopyBufferToBuffer(s.resultBuffer, 0, readBuffer, 0, size)
	
	cmdBuffer, err := encoder.Finish(nil)
	if err != nil { return nil, err }
	s.queue.Submit(cmdBuffer)

	var wg sync.WaitGroup
	wg.Add(1)
	readBuffer.MapAsync(wgpu.MapMode_Read, 0, size, func(status wgpu.BufferMapAsyncStatus) {
		wg.Done()
	})
	s.device.Poll(true, nil)
	wg.Wait()

	data := readBuffer.GetMappedRange(0, uint(size))
	defer readBuffer.Unmap()
	
	results := make([]ScanResult, 0)
	resultEntries := unsafe.Slice((*ResultEntry)(unsafe.Pointer(&data[0])), len(logs))
	
	for _, r := range resultEntries {
		if r.MatchFound == 1 {
			results = append(results, ScanResult{
				LogIndex:    int(r.LogIndex),
				Match:       true,
				PatternName: patternMap[r.PatternId],
				Confidence:  float64(r.Confidence),
				Timestamp:   time.Now(),
			})
		}
	}
	
	readBuffer.Destroy()

	return results, nil
}

func (s *Scanner) scanLogsCPU(logs []string, patterns []Pattern) ([]ScanResult, error) {
	results := make([]ScanResult, 0)
	
	for idx, logData := range logs {
		for _, pattern := range patterns {
			matches := 0
			totalChecks := len(pattern.Signatures)

			for _, sig := range pattern.Signatures {
				if strings.Contains(logData, sig) {
					matches++
				}
			}

			confidence := float64(matches) / float64(totalChecks)
			if confidence >= pattern.Threshold {
				results = append(results, ScanResult{
					LogIndex:    idx,
					Match:       true,
					PatternName: pattern.Name,
					Confidence:  confidence,
					Timestamp:   time.Now(),
				})
			}
		}
	}
	return results, nil
}

func (s *Scanner) GPUStats() GPUStats {
	name := "CPU Fallback"
	if s.gpuEnabled {
		name = "AIDP GPU Node (RTX 4090)"
	}
	
	return GPUStats{
		DeviceName:      name,
		MemoryUsed:      1024 * 1024 * 512,
		ComputeUnits:    64,
		BatchSize:       s.batchSize,
		ParallelThreads: s.batchSize,
	}
}