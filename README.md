BWiki
=====

Re-implemented in Golang, this is a simple, get-the-job-done wiki meeting my own needs.  If it makes you happy too, great.  The syntax is Markdown, WikiWords become links, just like in [the original Wiki](//wiki.c2.com).

Additionally, pages can be linked with [[...]] syntax:

- \[[Wiki page]]
- \[[wiki page|link text]]
- [[?DontLinkThis]]

There are no users, no security.  If you need it, a proxying web server (e.g. Apache) will have to provide it.


Versioning
==========

Page versions are stored in full.  The current version of a page is stored by name under pages/.  Older versions of a page are stored as old/_page_._ver_.  Deleted pages go under deleted/.

This storage scheme is wasteful compared to diffs, but Good Enough For Now.  In the future I might store old versions with some delta format.
