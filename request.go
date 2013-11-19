package web

import (
  "net/http"
  "reflect"
)

type Request struct {
  *http.Request
  
  // This map exists if you have wildcards in your URL that you need to capture.
  // Eg, /users/:id/tickets/:ticket_id and /users/1/tickets/33 would yield the map {id: "3", ticket_id: "33"}
  UrlVariables map[string]string
  
  // The actual route that got invoked
  route *Route
  
  // The target context corresponding to the route.
  context reflect.Value
}