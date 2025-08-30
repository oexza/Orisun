#!/bin/bash

# Script to collect benchmark results and calculate RPS
echo "Collecting Orisun Benchmark Results"
echo "===================================="

# Function to run a single benchmark and extract results
run_benchmark() {
    local benchmark_name=$1
    local benchtime=${2:-"3s"}
    
    echo "\nRunning $benchmark_name..."
    result=$(go test -bench=$benchmark_name -benchtime=$benchtime -count=1 ./benchmark_test.go 2>/dev/null | grep "^$benchmark_name")
    
    if [ -n "$result" ]; then
        # Extract iterations and ns/op
        iterations=$(echo $result | awk '{print $2}')
        ns_per_op=$(echo $result | awk '{print $3}' | sed 's/ns\/op//')
        
        # Calculate requests per second
        if [ "$ns_per_op" != "" ] && [ "$ns_per_op" != "0" ]; then
            rps=$(echo "scale=2; 1000000000 / $ns_per_op" | bc -l)
            echo "$benchmark_name: $iterations iterations, ${ns_per_op} ns/op, ${rps} req/s"
        else
            echo "$benchmark_name: Failed to extract timing data"
        fi
    else
        echo "$benchmark_name: Failed to run or no results"
    fi
}

# Install bc if not available (for calculations)
if ! command -v bc &> /dev/null; then
    echo "Installing bc for calculations..."
    brew install bc 2>/dev/null || echo "Please install bc manually"
fi

# Run individual benchmarks
run_benchmark "BenchmarkSaveEvents_Single" "2s"
run_benchmark "BenchmarkGetEvents" "2s"
run_benchmark "BenchmarkMemoryUsage" "2s"

echo "\nBenchmark collection complete!"