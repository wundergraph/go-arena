# go-arena - Arena memory allocation library for Go

go-arena is a library for allocating memory in chunks instead of making many small allocations.

If you're building HTTP servers in Go, sometimes you have to allocate many small objects throughout the request handling lifecycle.
These allocations need to be valid as long as the request is handled.
Instead of making these small allocations one at a time,
you can allocate a larger chunk of memory and use it across the entire lifecycle of the request.

go-arena works best when you can predict how much memory you'll need.
E.g. if you can look at request headers or the body and then estimate how much memory you'll need.

In addition, you'd want to use go-arena in combination with Weak Pointers.
This way, you can allocate an arena for a request, and when the request is done,
you can reset the arena and create a weak pointer to it.
If another request comes in before the next GC cycle,
you can just reuse the arena, otherwise the GC will clean it up.

This patterns keeps memory usage low and predictable while avoiding the number of small allocations.
In our case, using the arena pattern reduces CPU usage by 50% and latency by 20% while increasing memory usage by 30%.

## Arena Lifecycle

The Arena interface provides two methods for managing memory:

- **`Reset()`**: Resets the arena's state without releasing the underlying memory. This allows the arena to be reused for new allocations while keeping the allocated buffers in memory for better performance.

- **`Release()`**: Releases the arena's underlying memory back to the system. After calling this method, the arena should not be used for further allocations.

### When to use Reset() vs Release()

- Use **`Reset()`** when you want to reuse the arena for multiple allocation cycles (e.g., processing multiple requests in a server)
- Use **`Release()`** when you're completely done with the arena and want to free up memory

## Usage

### Basic Arena Allocation

```go
package main

import (
    "fmt"
    
    "github.com/wundergraph/go-arena"
)

type User struct {
    ID   int
    Name string
}

func main() {
    // Create a monotonic arena with default settings
    a := arena.NewMonotonicArena()
	defer a.Release()
    
    // Allocate a User struct using the arena
    user := arena.Allocate[User](a)
    user.ID = 1
    user.Name = "John Doe"
    
    // Allocate a slice using the arena
    users := arena.AllocateSlice[User](a, 0, 10) // length 0, capacity 10
    
    // Append users to the slice
    users = arena.SliceAppend(a, users, *user)
    
    fmt.Printf("User: %+v\n", user)
    fmt.Printf("Users slice: %+v\n", users)
    fmt.Printf("Arena stats - Len: %d, Cap: %d, Peak: %d\n", 
        a.Len(), a.Cap(), a.Peak())
}
```

### HTTP Request Handler Example

```go
package main

import (
    "encoding/json"
    "net/http"
    
    "github.com/wundergraph/go-arena"
)

type RequestData struct {
    Items []string `json:"items"`
}

type ResponseData struct {
    ProcessedItems []string `json:"processed_items"`
    Count          int      `json:"count"`
}

func handleRequest(w http.ResponseWriter, r *http.Request) {
    // Create arena for this request
    a := arena.NewMonotonicArena(
        arena.WithMinBufferSize(64 * 1024), // 64KB buffer
        arena.WithInitialBufferCount(1), // default, can be omitted
    )

    defer a.Release()
    
    // Parse request body
    var reqData RequestData
    if err := json.NewDecoder(r.Body).Decode(&reqData); err != nil {
        http.Error(w, "Invalid JSON", http.StatusBadRequest)
        return
    }
    
    // Allocate response structure using arena
    respData := arena.Allocate[ResponseData](a)
    respData.ProcessedItems = arena.AllocateSlice[string](a, 0, len(reqData.Items))
    
    // Process items
    for _, item := range reqData.Items {
        processed := "processed_" + item
        respData.ProcessedItems = arena.SliceAppend(a, respData.ProcessedItems, processed)
    }
    respData.Count = len(respData.ProcessedItems)
    
    // Send response
    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(respData)
}
```

### Concurrent Usage

```go
package main

import (
    "fmt"
    "sync"
    
    "github.com/wundergraph/go-arena"
)

func main() {
    // Create a base arena
    baseArena := arena.NewMonotonicArena()
    defer baseArena.Release()
    
    // Wrap it for concurrent access
    concurrentArena := arena.NewConcurrentArena(baseArena)
    
    var wg sync.WaitGroup
    
    // Multiple goroutines can safely use the same arena
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            
            // Allocate data using the concurrent arena
            data := arena.Allocate[int](concurrentArena)
            *data = id * 100
            
            fmt.Printf("Goroutine %d allocated: %d\n", id, *data)
        }(i)
    }
    
    wg.Wait()

}
```

### Custom Buffer Operations

```go
package main

import (
    "fmt"
    
    "github.com/wundergraph/go-arena"
)

func main() {
    a := arena.NewMonotonicArena()
    defer a.Release()
    
    // Create a buffer backed by the arena
    buf := arena.NewArenaBuffer(a)
    
    // Write data to buffer
    buf.WriteString("Hello, ")
    buf.WriteString("World!")
    
    // Read from buffer
    data := make([]byte, 5)
    buf.Read(data)
    fmt.Printf("Read: %s\n", string(data)) // "Hello"
    
    // Get remaining content
    fmt.Printf("Remaining: %s\n", buf.String()) // ", World!"
    
}
```

## License

go-arena is licensed under the Apache License 2.0.