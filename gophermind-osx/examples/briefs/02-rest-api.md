# Brief: shortlink service

A small REST API for shortening URLs. POST a long URL, get back a short
code; GET the short code, get redirected to the original URL. Codes
should be short (5-6 characters), not guessable in sequence, and should
not collide.

Needs basic abuse protection -- a rate limit per IP on creating new
links -- and a way to see how many times a given short link has been
visited.

Storage can be whatever's simplest to run and test against; this doesn't
need to scale past a few thousand links for now, just needs to be
correct and have real tests, not just work in the demo.
