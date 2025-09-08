#!/bin/bash

# Script to collect benchmark results and calculate RPS
echo "Collecting Orisun Benchmark Results"
echo "===================================="

# Function to run a single benchmark and extract results
run_benchmark() {
    local benchmark_name=$1
    local benchtime=${2:-"3s"}
    
    echo "\nRunning $benchmark_name..."
    # Run the benchmark and capture all output, then filter for benchmark results
    output=$(go test -bench=$benchmark_name -benchtime=$benchtime -count=1 ./benchmark_test.go 2>&1)
    
    # Extract benchmark result lines (they contain ns/op and events/sec)
    results=$(echo "$output" | grep -E "^Benchmark.*ns/op.*events/sec")
    
    if [ -n "$results" ]; then
        echo "$results"
        echo "\nSummary for $benchmark_name:"
        # Count total benchmark lines
        count=$(echo "$results" | wc -l | tr -d ' ')
        echo "  - $count sub-benchmarks completed"
        
        # Extract and show the best performance (highest events/sec)
        best_line=$(echo "$results" | awk '{print $NF, $0}' | sort -nr | head -1 | cut -d' ' -f2-)
        if [ -n "$best_line" ]; then
            best_events_sec=$(echo "$best_line" | awk '{print $NF}')
            echo "  - Best performance: $best_events_sec"
        fi
    else
        echo "$benchmark_name: No benchmark results found"
        echo "Error output:"
        echo "$output" | tail -10
    fi
}

# Install bc if not available (for calculations)
if ! command -v bc &> /dev/null; then
    echo "Installing bc for calculations..."
    brew install bc 2>/dev/null || echo "Please install bc manually"
fi

# Run individual benchmarks
run_benchmark "BenchmarkSaveEvents_Single" "1s"
run_benchmark "BenchmarkSaveEvents_Batch" "1s"
run_benchmark "BenchmarkGetEvents" "1s"
run_benchmark "BenchmarkMemoryUsage" "1s"

echo "\nBenchmark collection complete!"