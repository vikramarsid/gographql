# gographql

This project is a fork of the original [machinebox/graphql](https://github.com/machinebox/graphql) library, which is no longer being actively maintained. We've created this fork to address any issues or add new features that may be necessary.

Low-level GraphQL client for Go.

* Simple, familiar API
* Respects `context.Context` timeouts and cancellation
* Build and execute any kind of GraphQL request
* Use strong Go types for response data
* Use variables and upload files
* Simple error handling

## Installation
Make sure you have a working Go environment. To install graphql, simply run:

```
$ go get github.com/vikramarsid/gographql
```

## Usage

```go
import "context"

// create a client (safe to share across requests)
client := graphql.NewClient("https://vikramarsid.io/graphql")

// make a request
req := graphql.NewRequest(`
    query ($key: String!) {
        items (id:$key) {
            field1
            field2
            field3
        }
    }
`)

// set any variables
req.Var("key", "value")

// set header fields
req.Header.Set("Cache-Control", "no-cache")

// define a Context for the request
ctx := context.Background()

// run it and capture the response
var respData ResponseStruct
if err := client.Run(ctx, req, &respData); err != nil {
    log.Fatal(err)
}
```

### File support via multipart form data

By default, the package will send a JSON body. To enable the sending of files, you can opt to
use multipart form data instead using the `UseMultipartForm` option when you create your `Client`:

```
client := graphql.NewClient("https://vikramarsid.io/graphql", graphql.UseMultipartForm())
```

## Acknowledgements

Thanks to the original authors of [machinebox/graphql](https://github.com/machinebox/graphql) for their work on the library.
