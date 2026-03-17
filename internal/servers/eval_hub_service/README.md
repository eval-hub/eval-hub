# HTTP server code

The goal of the code organization is to keep all references to the package "net/http" in
this package so that if we switch to a fasthttp implementation we have all the HTTP related
code in a single package.
