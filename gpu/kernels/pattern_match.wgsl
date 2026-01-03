// LogStellar Pattern Matching Kernel
// Optimized for high-throughput substring search on GPU

// Structure for a single log entry (fixed size for GPU memory alignment)
// We treat logs as fixed-size byte arrays for parallel processing.
// In a production environment, we might use a texture or storage buffer of uint32s for variable lengths,
// but for this prototype, we use a fixed window.
const MAX_LOG_SIZE: u32 = 256; 
const MAX_PATTERNS: u32 = 32;
const MAX_PATTERN_LEN: u32 = 32;

struct LogEntry {
    data: array<u32, 64>, // 256 bytes packed into 64 u32s
    length: u32,
    padding: array<u32, 3>, // Align to 16 bytes
};

struct Pattern {
    data: array<u32, 8>, // 32 bytes packed into 8 u32s
    length: u32,
    id: u32,
    padding: array<u32, 2>, // Align to 16 bytes
};

struct Result {
    match_found: u32,      // 1 if match, 0 if no match
    pattern_id: u32,       // ID of the matched pattern
    confidence: f32,       // 1.0 for exact match
    log_index: u32,        // Index of the log that matched
};

// Bindings
@group(0) @binding(0) var<storage, read> logs: array<LogEntry>;
@group(0) @binding(1) var<storage, read> patterns: array<Pattern>;
@group(0) @binding(2) var<storage, read_write> results: array<Result>;
@group(0) @binding(3) var<uniform> config: vec2<u32>; // x: num_logs, y: num_patterns

// Helper to unpack a byte from a u32 array
fn get_byte(data: array<u32, 64>, index: u32) -> u32 {
    let word_idx = index / 4u;
    let byte_idx = index % 4u;
    let word = data[word_idx];
    return (word >> (byte_idx * 8u)) & 0xFFu;
}

fn get_pattern_byte(data: array<u32, 8>, index: u32) -> u32 {
    let word_idx = index / 4u;
    let byte_idx = index % 4u;
    let word = data[word_idx];
    return (word >> (byte_idx * 8u)) & 0xFFu;
}

@compute @workgroup_size(64)
fn main(@builtin(global_invocation_id) global_id: vec3<u32>) {
    let log_idx = global_id.x;
    
    // Bounds check
    if (log_idx >= config.x) {
        return;
    }

    let log_len = logs[log_idx].length;
    let num_patterns = config.y;

    // Initialize result for this log
    results[log_idx].match_found = 0u;
    results[log_idx].confidence = 0.0;
    results[log_idx].log_index = log_idx;

    // Iterate through all loaded patterns
    for (var p_idx = 0u; p_idx < num_patterns; p_idx = p_idx + 1u) {
        let pat_len = patterns[p_idx].length;
        
        // Optimization: Don't search if log is shorter than pattern
        if (log_len < pat_len) {
            continue;
        }

        // Brute-force substring search (Naive but effective for massively parallel GPU)
        // For every possible start position in the log...
        for (var i = 0u; i <= (log_len - pat_len); i = i + 1u) {
            var match = true;
            
            // ...check if the pattern matches
            for (var j = 0u; j < pat_len; j = j + 1u) {
                let log_char = get_byte(logs[log_idx].data, i + j);
                let pat_char = get_pattern_byte(patterns[p_idx].data, j);
                
                if (log_char != pat_char) {
                    match = false;
                    break;
                }
            }

            if (match) {
                // Write result directly to global memory
                // In a real scenario, we might use atomics or handle multiple matches,
                // but for this bounty prototype, capturing the *first* high-value match is sufficient.
                results[log_idx].match_found = 1u;
                results[log_idx].pattern_id = patterns[p_idx].id;
                results[log_idx].confidence = 1.0;
                return; // Early exit on first match for this log
            }
        }
    }
}
