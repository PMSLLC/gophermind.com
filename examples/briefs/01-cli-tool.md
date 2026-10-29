# Brief: link-check

A command-line tool that reads a Markdown or HTML file, extracts every
link, and checks whether each one still resolves (HTTP 200, or a
redirect that eventually resolves). It should print a short report:
which links are fine, which are broken, and which redirected somewhere
new.

Meant to run in CI on a docs repo, so it needs to exit non-zero when it
finds a broken link, and finish in a reasonable time even with a few
hundred links (checked concurrently, not one at a time).

Nice to have, not required: a `--fix` mode that rewrites redirected links
to their final destination in place.
