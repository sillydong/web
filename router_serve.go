package web

import (
  "reflect"
  "net/http"
  "fmt"
  "runtime"
)

func (rootRouter *Router) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
  
  // Wrap the request and writer.
  responseWriter := &ResponseWriter{rw}
  request := &Request{Request: r}
  
  // Handle errors
  defer func() {
    if recovered := recover(); recovered != nil {
      rootRouter.handlePanic(responseWriter, request, recovered) // TODO: that's wrong (used to be route.Router)
    }
  }()
  
  middlewareStack := rootRouter.MiddlewareStackV2(responseWriter, request)
  middlewareStack()
}

// r should be the root router
func (r *Router) MiddlewareStackV2(rw *ResponseWriter, req *Request) NextMiddlewareFunc {
  fmt.Println("YAY ENTERING MY SHIT")
  // Where are we in the stack
  routers := []*Router{r}
  contexts := []reflect.Value{reflect.New(r.contextType)}
  currentMiddlewareIndex := 0
  currentRouterIndex := 0
  currentMiddlewareLen := len(r.middleware)
  
  // Pre-make some Values
  vrw := reflect.ValueOf(rw)
  vreq := reflect.ValueOf(req)
  
  var next NextMiddlewareFunc // create self-referential anonymous function
  var nextValue reflect.Value
  next = func() {
    if currentRouterIndex >= len(routers) {
      return
    }
    
    // Find middleware to invoke. The goal of this block is to set the middleware variable. If it can't be done, it will be the zero value.
    // Side effects of this loop: set currentMiddlewareIndex, currentRouterIndex, currentMiddlewareLen
    var middleware reflect.Value
    if currentMiddlewareIndex < currentMiddlewareLen {
      middleware = routers[currentRouterIndex].middleware[currentMiddlewareIndex]
    } else {
      // We ran out of middleware on the current router
      if currentRouterIndex == 0 {
        // If we're still on the root router, it's time to actually figure out what the route is.
        // Do so, and update the various variables.
        // We could also 404 at this point: if so, run NotFound handlers and return.
        route, wildcardMap := calculateRoute(r, req)
        if route == nil {
          panic("404")
          return
        }
        
        req.route = route
        req.UrlVariables = wildcardMap
        
        routers = routersFor(route)
        fmt.Println("Ok I got all the routers dog: ", routers)
        contexts = append(contexts, additionalContexts(contexts[0], routers)...)
      }
      
      currentMiddlewareIndex = 0
      currentRouterIndex += 1
      routersLen := len(routers)
      for currentRouterIndex < routersLen {
        currentMiddlewareLen = len(routers[currentRouterIndex].middleware)
        if currentMiddlewareLen > 0 {
          break
        }
        currentRouterIndex += 1
      }
      if currentRouterIndex < routersLen {
        middleware = routers[currentRouterIndex].middleware[currentMiddlewareIndex]
      } else {
        // We're done! invoke the action
        invoke(req.route.Handler, contexts[len(contexts) - 1], []reflect.Value{vrw, vreq})
      }
    }
    
    currentMiddlewareIndex += 1
    
    // Invoke middleware. Reflect on the function to call the context or no-context variant.
    if middleware.IsValid() {
      invoke(middleware, contexts[currentRouterIndex], []reflect.Value{vrw, vreq, nextValue})
    }
  }
  nextValue = reflect.ValueOf(next)
  
  return next
}

func calculateRoute(rootRouter *Router, req *Request) (*Route, map[string]string) {
  var leaf *PathLeaf
  var wildcardMap map[string]string
  tree, ok := rootRouter.root[HttpMethod(req.Method)]
  if ok {
    leaf, wildcardMap = tree.Match(req.URL.Path)
  }
  if leaf == nil {
    return nil, nil
  } else {
    return leaf.route, wildcardMap
  }
}

func routersFor(route *Route) []*Router {
  var routers []*Router
  curRouter := route.Router
  for curRouter != nil {
    routers = append(routers, curRouter)
    curRouter = curRouter.parent
  }
  
  // Reverse the slice
  s := 0
  e := len(routers) - 1
  for s < e {
    routers[s], routers[e] = routers[e], routers[s]
    s += 1
    e -= 1
  }
  
  return routers
}

func additionalContexts(rootContext reflect.Value, routers []*Router) []reflect.Value {
  return nil 
}



//
// BEGIN OLD CODE
// 



// This is the main entry point for a request from the built-in Go http library.
// router should be the root router.
func (rootRouter *Router) ServeHTTPV1(rw http.ResponseWriter, r *http.Request) {
  
  // Wrap the request and writer.
  responseWriter := &ResponseWriter{rw}
  request := &Request{Request: r}
  
  // Do routing
  var leaf *PathLeaf
  var wildcardMap map[string]string
  tree, ok := rootRouter.root[HttpMethod(r.Method)]
  if ok {
    leaf, wildcardMap = tree.Match(r.URL.Path)
  }
  if leaf == nil {
    if rootRouter.notFoundHandler != nil {
      rw.WriteHeader(http.StatusNotFound)
      rootRouter.notFoundHandler(responseWriter, request)
    } else {
      http.Error(rw, DefaultNotFoundResponse, http.StatusNotFound)
    }
    return
  }
  
  route := leaf.route
  request.route = route
  request.UrlVariables = wildcardMap
  
  // Handle errors
  defer func() {
    if recovered := recover(); recovered != nil {
      route.Router.handlePanic(responseWriter, request, recovered)
    }
  }()
  
  middlewareStack := route.Router.MiddlewareStack(responseWriter, request)
  middlewareStack()
}

// This is called against the *target* router
func (targetRouter *Router) handlePanic(rw *ResponseWriter, req *Request, err interface{}) {
  
  // Find the first router that has an errorHandler
  // We also need to get the context corresponding to that router.
  curRouter := targetRouter
  curContextPtr := req.context
  for !curRouter.errorHandler.IsValid() && curRouter.parent != nil {
    curRouter = curRouter.parent
    
    // Need to set curContext to the next context, UNLESS the context is the same type.
    curContextStruct := reflect.Indirect(curContextPtr)
    if curRouter.contextType != curContextStruct.Type() {
      curContextPtr = curContextStruct.Field(0)
      if reflect.Indirect(curContextPtr).Type() != curRouter.contextType {
        panic("oshit why")
      }
    }
  }
  
  if curRouter.errorHandler.IsValid() {
    rw.WriteHeader(http.StatusInternalServerError)
    invoke(curRouter.errorHandler, curContextPtr, []reflect.Value{reflect.ValueOf(rw), reflect.ValueOf(req), reflect.ValueOf(err)})
  } else {
    http.Error(rw, DefaultPanicResponse, http.StatusInternalServerError)
  }
  
  const size = 4096
  stack := make([]byte, size)
  stack = stack[:runtime.Stack(stack, false)]
  
  ERROR.Printf("%v\n", err)
  ERROR.Printf("%s\n", string(stack))
}

// This is the last middleware. It will just invoke the action
func RouteInvokingMiddleware(rw *ResponseWriter, req *Request, next NextMiddlewareFunc) {
  req.route.Handler.Call([]reflect.Value{req.context, reflect.ValueOf(rw), reflect.ValueOf(req)})
}

// Routers is a [leaf, child, ... , root]. Return [ctx for leaf, ctx for child, ..., ctx for root]
// Routers must have at least one element
func createContexts(routers []*Router) (contexts []reflect.Value) {
  routersLen := len(routers)
  
  contexts = make([]reflect.Value, routersLen)
  
  for i := routersLen - 1; i >= 0; i -= 1 {
    ctx := reflect.New(routers[i].contextType)
    contexts[i] = ctx
    
    // If we're not the root context, then set the first field to the parent
    if i < routersLen - 1 {
      f := reflect.Indirect(ctx).Field(0)
      f.Set(contexts[i + 1])
    }
  }
  
  return
}

func (r *Router) MiddlewareStack(rw *ResponseWriter, req *Request) NextMiddlewareFunc {
  // r is the target router (could be a leaf router, or the root router, or somewhere in between)
  // Construct routers, being [leaf, child, ..., root]
  var routers []*Router
  curRouter := r
  for curRouter != nil {
    routers = append(routers, curRouter)
    curRouter = curRouter.parent
  }
  
  // contexts are parallel to routers. We're going to pre-emptively create all contexts
  var contexts []reflect.Value
  contexts = createContexts(routers)
  req.context = contexts[0]
  
  // Inputs into next():
  // routers: 1 or more routers in reverse order
  // currentRouterIndex: N-1, ..., 0. If -1, then we're done
  // currentMiddlwareLen: len(routers[currentRouterIndex].middleware)
  // currentMiddlewareIndex: 0, ..., len(routers[currentRounterIndex]). We *CAN* enter next() with this out of bounds. That's expected.
  currentRouterIndex := len(routers) - 1
  currentMiddlewareLen := len(routers[currentRouterIndex].middleware)
  currentMiddlewareIndex := 0
  
  var next NextMiddlewareFunc // create self-referential anonymous function
  var nextValue reflect.Value
  
  // Pre-make some Values
  vrw := reflect.ValueOf(rw)
  vreq := reflect.ValueOf(req)
  
  next = func() {
    if currentRouterIndex < 0 {
      return
    }
    
    // Find middleware to invoke. The goal of the if statement is to set the middleware variable. If it can't be done, it will be the zero value.
    // Side effects of this loop: set currentMiddlewareIndex, currentRouterIndex
    var middleware reflect.Value
    if currentMiddlewareIndex < currentMiddlewareLen {
      // It's in bounds? Cool, use it
      middleware = routers[currentRouterIndex].middleware[currentMiddlewareIndex]
    } else {
      // Out of bounds. Find next router with middleware. If none, use the invoking middleware
      currentMiddlewareIndex = 0
      for {
        currentRouterIndex -= 1
        
        if currentRouterIndex < 0 {
          // If we're at the end of the routers, invoke the final virtual middleware: the handler invoker.
          // (next() wont execute on future calls b/c we'll return at the top)
          middleware = reflect.ValueOf(RouteInvokingMiddleware)
          break
        }
        
        // So currentRouterIndex >= 0 b/c we didn't break.
        currentMiddlewareLen = len(routers[currentRouterIndex].middleware)
        
        if currentMiddlewareLen > 0 {
          middleware = routers[currentRouterIndex].middleware[currentMiddlewareIndex]
          break
        }
        // didn't break? loop
      }
    }
    
    // Make sure we increment the index for the next time
    currentMiddlewareIndex += 1
    
    // Invoke middleware. Reflect on the function to call the context or no-context variant.
    if middleware.IsValid() {
      var ctx reflect.Value
      if currentRouterIndex >= 0 {
        ctx = contexts[currentRouterIndex]
      }
      invoke(middleware, ctx, []reflect.Value{vrw, vreq, nextValue})
    }
  }
  nextValue = reflect.ValueOf(next)
  
  return next
}

func invoke(handler reflect.Value, ctx reflect.Value, values []reflect.Value) {
  handlerType := handler.Type()
  numIn := handlerType.NumIn()
  if numIn == len(values) {
    handler.Call(values)
  } else {
    values = append([]reflect.Value{ctx}, values...)
    handler.Call(values)
  }
}

var DefaultNotFoundResponse string = "Not Found"
var DefaultPanicResponse string = "Application Error"

