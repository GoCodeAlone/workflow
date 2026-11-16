# One-time v0.74.8 release admission

The normal protected-main ancestry guard remains. Its only exception is the
reviewed JWT backport in PR #1041, released as `v0.74.8` from `maintenance/v0.74`.
No repository protection or merge setting needs to change.

1. Merge the equivalent JWT fix to main through its reviewed PR. Its auth patch
   must equal the reviewed backport (`50ca17cb77067ddc36b5583bb54fc62a4f7cc5db`).
2. Complete exact-head Maintenance JWT CI and merge PR #1041 to
   `maintenance/v0.74`. Record the reviewed PR head, successful CI run and actual
   merge commit. Squash is supported only when the merge tree exactly equals
   the reviewed source tree.
3. Open a separate main PR adding
   `.github/release-admissions/v0.74.8.json` with these fields:

   ```json
   {
     "schema": 1,
     "tag": "v0.74.8",
     "release_commit": "<actual maintenance merge commit>",
     "source_commit": "<reviewed PR1041 head>",
     "release_tree": "<identical release and reviewed source tree>",
     "base_commit": "f03ce511ea6fbc5daab4f9b828bc3c2cb762fa43",
     "source_pr": 1041,
     "source_ci_run": 0,
     "upstream_pr": 0,
     "upstream_commit": "<JWT fix merge commit on main>"
   }
   ```

   Replace placeholders and zero identifiers with the exact evidence. Review
   the full maintenance source and CI provenance before merging this record.
   Placeholders are deliberately invalid. A record in the tagged source, a PR
   branch or an unmerged main PR cannot authorize release.
4. After owner-authorized record merge and tag publication, the release guard
   fetches main, the maintained branch and PR1041 source; verifies both merged
   PRs, base ancestry, identical trees, the exact JWT patch and successful
   source CI; and admits only that recorded commit. Missing records, unavailable
   GitHub reads or mismatches deny release before tests/builds/publication.

No tag is created by this change. Tagging before admission will fail. The
existing release tests and artifact gates remain; maintenance images use only
exact version/full-SHA tags, GitHub latest stays false, and downstream broadcasts
and moving aliases remain absent. Other tags gain no maintenance exception.
