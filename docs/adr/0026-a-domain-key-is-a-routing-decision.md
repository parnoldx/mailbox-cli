# A domain key is a routing decision

A Destination can be about one address or about every address at a domain
(`@stripe.com`). The script is still the record (ADR-0019): domain rules are
written as `address :domain :is "from"`, after every address rule, so Sieve's
first match is the two-pass — a specific address always wins.

The alternative was client-side list files that the daemon would apply after
mail arrived. That files too late (the Screener already has the mail) and
splits the record: the script would no longer be what the server runs. Sieve
already has `:domain`. Using it keeps one script, one write, one projection.

A domain that could not be put in a quoted string safely is refused, for the
same reason an address is: the value comes from a From header.
