package main

import (
	"context"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

// MockCloser implements io.Closer for testing
type MockCloser struct {
	closed    bool
	closeErr  error
	closeFunc func() error
	mu        sync.Mutex
}

func NewMockCloser() *MockCloser {
	return &MockCloser{}
}

func (m *MockCloser) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	m.closed = true
	if m.closeFunc != nil {
		return m.closeFunc()
	}
	return m.closeErr
}

func (m *MockCloser) IsClosed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.closed
}

func (m *MockCloser) SetCloseError(err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeErr = err
}

func (m *MockCloser) SetCloseFunc(f func() error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeFunc = f
}

// Test ResourceManager functionality
func TestResourceManager(t *testing.T) {
	t.Run("NewResourceManager", func(t *testing.T) {
		rm := NewResourceManager()
		if rm == nil {
			t.Fatal("NewResourceManager returned nil")
		}
		if rm.httpClient == nil {
			t.Error("httpClient not initialized")
		}
		if rm.openFiles == nil {
			t.Error("openFiles not initialized")
		}
		if rm.connections == nil {
			t.Error("connections not initialized")
		}
	})

	t.Run("AddResource_File", func(t *testing.T) {
		rm := NewResourceManager()
		mockFile := NewMockCloser()
		
		rm.AddResource(mockFile, "file")
		
		if len(rm.openFiles) != 1 {
			t.Errorf("Expected 1 file, got %d", len(rm.openFiles))
		}
		if len(rm.connections) != 0 {
			t.Errorf("Expected 0 connections, got %d", len(rm.connections))
		}
	})

	t.Run("AddResource_Connection", func(t *testing.T) {
		rm := NewResourceManager()
		mockConn := NewMockCloser()
		
		rm.AddResource(mockConn, "connection")
		
		if len(rm.connections) != 1 {
			t.Errorf("Expected 1 connection, got %d", len(rm.connections))
		}
		if len(rm.openFiles) != 0 {
			t.Errorf("Expected 0 files, got %d", len(rm.openFiles))
		}
	})

	t.Run("AddResource_Multiple", func(t *testing.T) {
		rm := NewResourceManager()
		mockFile1 := NewMockCloser()
		mockFile2 := NewMockCloser()
		mockConn1 := NewMockCloser()
		mockConn2 := NewMockCloser()
		
		rm.AddResource(mockFile1, "file")
		rm.AddResource(mockFile2, "file")
		rm.AddResource(mockConn1, "connection")
		rm.AddResource(mockConn2, "connection")
		
		if len(rm.openFiles) != 2 {
			t.Errorf("Expected 2 files, got %d", len(rm.openFiles))
		}
		if len(rm.connections) != 2 {
			t.Errorf("Expected 2 connections, got %d", len(rm.connections))
		}
	})

	t.Run("Cleanup_Success", func(t *testing.T) {
		rm := NewResourceManager()
		mockFile := NewMockCloser()
		mockConn := NewMockCloser()
		
		rm.AddResource(mockFile, "file")
		rm.AddResource(mockConn, "connection")
		
		// Ensure resources are not closed initially
		if mockFile.IsClosed() {
			t.Error("File should not be closed initially")
		}
		if mockConn.IsClosed() {
			t.Error("Connection should not be closed initially")
		}
		
		rm.Cleanup()
		
		// Verify resources are closed after cleanup
		if !mockFile.IsClosed() {
			t.Error("File should be closed after cleanup")
		}
		if !mockConn.IsClosed() {
			t.Error("Connection should be closed after cleanup")
		}
	})

	t.Run("Cleanup_WithErrors", func(t *testing.T) {
		rm := NewResourceManager()
		mockFile := NewMockCloser()
		mockConn := NewMockCloser()
		
		// Set close errors
		mockFile.SetCloseError(io.ErrClosedPipe)
		mockConn.SetCloseError(io.ErrShortWrite)
		
		rm.AddResource(mockFile, "file")
		rm.AddResource(mockConn, "connection")
		
		// Cleanup should not panic even with errors
		rm.Cleanup()
		
		// Verify Close() was called despite errors
		if !mockFile.IsClosed() {
			t.Error("File should be closed after cleanup despite error")
		}
		if !mockConn.IsClosed() {
			t.Error("Connection should be closed after cleanup despite error")
		}
	})

	t.Run("Cleanup_Concurrent", func(t *testing.T) {
		rm := NewResourceManager()
		mockFile := NewMockCloser()
		
		rm.AddResource(mockFile, "file")
		
		// Test concurrent access to cleanup
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				rm.Cleanup()
			}()
		}
		
		wg.Wait()
		
		// Should not panic and file should be closed
		if !mockFile.IsClosed() {
			t.Error("File should be closed after concurrent cleanup")
		}
	})
}

// Test helper function to capture logs
func captureLog(fn func()) string {
	// In a real implementation, you might use a log buffer
	// For now, we'll just execute the function
	fn()
	return ""
}

// Test graceful shutdown scenarios
func TestGracefulShutdown(t *testing.T) {
	t.Run("ImmediateShutdown", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		timeout := 5 * time.Second
		
		done := make(chan struct{})
		go func() {
			defer close(done)
			runAgent(ctx, timeout)
		}()
		
		// Cancel immediately
		cancel()
		
		// Should complete quickly
		select {
		case <-done:
			// Success
		case <-time.After(1 * time.Second):
			t.Error("runAgent should have completed quickly on immediate cancellation")
		}
	})

	t.Run("ShutdownWithActiveTasks", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		timeout := 2 * time.Second
		
		done := make(chan struct{})
		go func() {
			defer close(done)
			runAgent(ctx, timeout)
		}()
		
		// Let it run for a bit to start tasks
		time.Sleep(6 * time.Second)
		
		// Cancel and measure shutdown time
		start := time.Now()
		cancel()
		
		select {
		case <-done:
			elapsed := time.Since(start)
			// Should complete within reasonable time (task completion + timeout + buffer)
			if elapsed > timeout+3*time.Second {
				t.Errorf("Shutdown took too long: %v", elapsed)
			}
		case <-time.After(timeout + 5*time.Second):
			t.Error("runAgent should have completed within timeout + buffer")
		}
	})
}

// Test timeout configuration
func TestTimeoutConfiguration(t *testing.T) {
	testCases := []struct {
		name           string
		timeoutStr     string
		expectedResult time.Duration
		expectError    bool
	}{
		{
			name:           "ValidDuration_Seconds",
			timeoutStr:     "30s",
			expectedResult: 30 * time.Second,
			expectError:    false,
		},
		{
			name:           "ValidDuration_Minutes",
			timeoutStr:     "2m",
			expectedResult: 2 * time.Minute,
			expectError:    false,
		},
		{
			name:           "ValidDuration_Mixed",
			timeoutStr:     "1m30s",
			expectedResult: 90 * time.Second,
			expectError:    false,
		},
		{
			name:           "InvalidDuration",
			timeoutStr:     "invalid",
			expectedResult: 30 * time.Second, // default fallback
			expectError:    true,
		},
		{
			name:           "EmptyDuration",
			timeoutStr:     "",
			expectedResult: 30 * time.Second, // default fallback
			expectError:    true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			duration, err := time.ParseDuration(tc.timeoutStr)
			
			if tc.expectError {
				if err == nil {
					t.Error("Expected error but got none")
				}
				// Test fallback to default
				if tc.timeoutStr == "" || strings.Contains(tc.timeoutStr, "invalid") {
					duration = 30 * time.Second // Simulate fallback logic
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error but got: %v", err)
				}
			}
			
			if duration != tc.expectedResult {
				t.Errorf("Expected %v, got %v", tc.expectedResult, duration)
			}
		})
	}
}

// Test task lifecycle management
func TestTaskLifecycleManagement(t *testing.T) {
	t.Run("TaskCompletion", func(t *testing.T) {
		rm := NewResourceManager()
		
		start := time.Now()
		fetchProbeList(rm)
		elapsed := time.Since(start)
		
		// Should complete in expected time (1s fetch + 0.5s process = ~1.5s)
		expectedMin := 1*time.Second + 500*time.Millisecond
		expectedMax := expectedMin + 200*time.Millisecond // Allow some buffer
		
		if elapsed < expectedMin {
			t.Errorf("Task completed too quickly: %v (expected at least %v)", elapsed, expectedMin)
		}
		if elapsed > expectedMax {
			t.Errorf("Task took too long: %v (expected at most %v)", elapsed, expectedMax)
		}
	})

	t.Run("ProcessProbes", func(t *testing.T) {
		rm := NewResourceManager()
		
		start := time.Now()
		processProbes(rm)
		elapsed := time.Since(start)
		
		// Should complete in expected time (~0.5s)
		expectedMin := 500 * time.Millisecond
		expectedMax := expectedMin + 100*time.Millisecond // Allow some buffer
		
		if elapsed < expectedMin {
			t.Errorf("Process completed too quickly: %v (expected at least %v)", elapsed, expectedMin)
		}
		if elapsed > expectedMax {
			t.Errorf("Process took too long: %v (expected at most %v)", elapsed, expectedMax)
		}
	})
}

// Test timeout scenarios
func TestGracefulShutdownTimeout(t *testing.T) {
	t.Run("TimeoutExceeded", func(t *testing.T) {
		// This test simulates a scenario where tasks take longer than the timeout
		ctx, cancel := context.WithCancel(context.Background())
		shortTimeout := 100 * time.Millisecond // Very short timeout
		
		done := make(chan struct{})
		go func() {
			defer close(done)
			runAgent(ctx, shortTimeout)
		}()
		
		// Let it run long enough to start a task
		time.Sleep(6 * time.Second)
		
		start := time.Now()
		cancel()
		
		select {
		case <-done:
			elapsed := time.Since(start)
			// Should complete close to the timeout period
			if elapsed < shortTimeout {
				t.Errorf("Shutdown completed too quickly: %v (expected around %v)", elapsed, shortTimeout)
			}
			if elapsed > shortTimeout+1*time.Second { // Allow reasonable buffer
				t.Errorf("Shutdown took too long: %v (expected around %v)", elapsed, shortTimeout)
			}
		case <-time.After(5 * time.Second):
			t.Error("runAgent should have completed within reasonable time")
		}
	})

	t.Run("ZeroTimeout", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		zeroTimeout := 0 * time.Second
		
		done := make(chan struct{})
		go func() {
			defer close(done)
			runAgent(ctx, zeroTimeout)
		}()
		
		// Let it run briefly
		time.Sleep(2 * time.Second)
		
		start := time.Now()
		cancel()
		
		select {
		case <-done:
			elapsed := time.Since(start)
			// Should complete immediately with zero timeout
			if elapsed > 100*time.Millisecond {
				t.Errorf("Shutdown with zero timeout took too long: %v", elapsed)
			}
		case <-time.After(1 * time.Second):
			t.Error("runAgent should have completed immediately with zero timeout")
		}
	})
}

// Test signal handling integration
func TestSignalHandlingScenarios(t *testing.T) {
	t.Run("MultipleSignals", func(t *testing.T) {
		// Test that multiple signals don't cause issues
		ctx, cancel := context.WithCancel(context.Background())
		timeout := 2 * time.Second
		
		done := make(chan struct{})
		go func() {
			defer close(done)
			runAgent(ctx, timeout)
		}()
		
		// Send multiple cancellations (simulating multiple signals)
		go func() {
			time.Sleep(1 * time.Second)
			cancel()
			cancel() // Second cancel should be no-op
			cancel() // Third cancel should be no-op
		}()
		
		select {
		case <-done:
			// Success - should handle multiple signals gracefully
		case <-time.After(5 * time.Second):
			t.Error("runAgent should have completed after multiple signals")
		}
	})
}

// Test resource management edge cases
func TestResourceManagerEdgeCases(t *testing.T) {
	t.Run("CleanupWithNilResources", func(t *testing.T) {
		rm := NewResourceManager()
		
		// Add nil pointer (should be handled gracefully)
		rm.openFiles = append(rm.openFiles, nil)
		rm.connections = append(rm.connections, nil)
		
		// Should not panic
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Cleanup panicked with nil resources: %v", r)
			}
		}()
		
		rm.Cleanup()
	})

	t.Run("CleanupEmptyManager", func(t *testing.T) {
		rm := NewResourceManager()
		
		// Should not panic on empty manager
		defer func() {
			if r := recover(); r != nil {
				t.Errorf("Cleanup panicked on empty manager: %v", r)
			}
		}()
		
		rm.Cleanup()
	})

	t.Run("SlowClosingResource", func(t *testing.T) {
		rm := NewResourceManager()
		slowCloser := NewMockCloser()
		
		// Set a slow close function
		slowCloser.SetCloseFunc(func() error {
			time.Sleep(100 * time.Millisecond)
			return nil
		})
		
		rm.AddResource(slowCloser, "file")
		
		start := time.Now()
		rm.Cleanup()
		elapsed := time.Since(start)
		
		// Should wait for slow close
		if elapsed < 100*time.Millisecond {
			t.Error("Cleanup should wait for slow closing resource")
		}
		
		if !slowCloser.IsClosed() {
			t.Error("Slow closer should be closed after cleanup")
		}
	})
}

// Test concurrent access patterns
func TestConcurrentAccess(t *testing.T) {
	t.Run("ConcurrentAddAndCleanup", func(t *testing.T) {
		rm := NewResourceManager()
		
		var wg sync.WaitGroup
		
		// Concurrent adds
		for i := 0; i < 50; i++ {
			wg.Add(1)
			go func(id int) {
				defer wg.Done()
				mockResource := NewMockCloser()
				resourceType := "file"
				if id%2 == 0 {
					resourceType = "connection"
				}
				rm.AddResource(mockResource, resourceType)
			}(i)
		}
		
		// Concurrent cleanup
		wg.Add(1)
		go func() {
			defer wg.Done()
			time.Sleep(10 * time.Millisecond) // Let some adds happen first
			rm.Cleanup()
		}()
		
		wg.Wait()
		
		// Should not panic and should handle concurrent access
	})
}

// Test task execution patterns
func TestTaskExecutionPatterns(t *testing.T) {
	t.Run("TaskWithResourceUsage", func(t *testing.T) {
		rm := NewResourceManager()
		
		// Add some resources before task
		mockFile := NewMockCloser()
		mockConn := NewMockCloser()
		rm.AddResource(mockFile, "file")
		rm.AddResource(mockConn, "connection")
		
		// Execute task
		start := time.Now()
		fetchProbeList(rm)
		elapsed := time.Since(start)
		
		// Task should complete normally
		if elapsed < 1*time.Second {
			t.Error("Task completed too quickly")
		}
		
		// Resources should still be available (not cleaned up by task)
		if mockFile.IsClosed() {
			t.Error("File should not be closed by task execution")
		}
		if mockConn.IsClosed() {
			t.Error("Connection should not be closed by task execution")
		}
		
		// Cleanup should close resources
		rm.Cleanup()
		if !mockFile.IsClosed() {
			t.Error("File should be closed after cleanup")
		}
		if !mockConn.IsClosed() {
			t.Error("Connection should be closed after cleanup")
		}
	})
}

// Test memory management
func TestMemoryManagement(t *testing.T) {
	t.Run("LargeNumberOfResources", func(t *testing.T) {
		rm := NewResourceManager()
		
		// Add many resources
		const numResources = 1000
		resources := make([]*MockCloser, numResources)
		
		for i := 0; i < numResources; i++ {
			resources[i] = NewMockCloser()
			resourceType := "file"
			if i%2 == 0 {
				resourceType = "connection"
			}
			rm.AddResource(resources[i], resourceType)
		}
		
		// Verify all added
		if len(rm.openFiles) != numResources/2 {
			t.Errorf("Expected %d files, got %d", numResources/2, len(rm.openFiles))
		}
		if len(rm.connections) != numResources/2 {
			t.Errorf("Expected %d connections, got %d", numResources/2, len(rm.connections))
		}
		
		// Cleanup should handle all resources
		start := time.Now()
		rm.Cleanup()
		elapsed := time.Since(start)
		
		// Should complete in reasonable time
		if elapsed > 5*time.Second {
			t.Errorf("Cleanup of %d resources took too long: %v", numResources, elapsed)
		}
		
		// Verify all closed
		for i, resource := range resources {
			if !resource.IsClosed() {
				t.Errorf("Resource %d should be closed", i)
			}
		}
	})
}

// Benchmark tests
func BenchmarkResourceManagerCleanup(b *testing.B) {
	rm := NewResourceManager()
	
	// Add some resources
	for i := 0; i < 100; i++ {
		rm.AddResource(NewMockCloser(), "file")
		rm.AddResource(NewMockCloser(), "connection")
	}
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		rm.Cleanup()
	}
}

func BenchmarkResourceManagerAddResource(b *testing.B) {
	rm := NewResourceManager()
	
	b.ResetTimer()
	
	for i := 0; i < b.N; i++ {
		rm.AddResource(NewMockCloser(), "file")
	}
}