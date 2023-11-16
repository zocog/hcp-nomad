# Contributing to Nomad Enterprise

Working on `nomad-enterprise` can be challenging.

For one thing, there is some code present in CE
(e.g. an CE CLI call that hits an ENT server)
that requires ENT features for test setup.

Also, in general merging should move in one direction, from CE to ENT,
which happens in GitHub Actions
nightly [here](https://github.com/hashicorp/nomad-enterprise/actions/workflows/merge-ce-cron.yaml)
and manually [here](https://github.com/hashicorp/nomad-enterprise/actions/workflows/merge-ce-manually.yaml).
See [README-ent.md](../README-ent.md#updating-enterprise-with-ce-code)
for more info about that.

Hopefully these many words will help ease some of the pain
that results from this arrangement.

## Editing code

Firstly, be sure you have your IDE configured to include the `ent` build flag,
otherwise many IDE features will not work.

It may be best to make all changes, both CE and ENT, here in `nomad-enterprise`,
so you can run tests here with `go test -tags=ent`, then afterwards port a
subset of the changes over to open source `nomad` repo.

If all of your changes are to ENT-only files and don't touch CE files at all,
you'll still need to make a changelog entry in CE with the ENT PR ID, e.g.
[hashicorp/nomad/.changelog/_839.txt](https://github.com/hashicorp/nomad/blob/main/.changelog/_839.txt),
but otherwise the below does not apply.

## Porting back to CE

You may try these methods to see what works for you.

1. Be diligent in making separate commits for CE files
   when working on an ENT branch, so you can cherry-pick them
   into an CE branch.
2. Go wild. Make changes and commits as feels natural to you,
   then produce a patch file from a `git diff` and selectively
   [`git apply`](https://git-scm.com/docs/git-apply)
   it in CE.

I do not have the discipline for option 1,
so I will elaborate here on option 2.

### Create a git patch

After committing all your changes (both CE and ENT) here in ENT, like

```
nomad-enterprise $ ls example 
example.go  example_ent.go  example_ce.go
nomad-enterprise $ git add example
nomad-enterprise $ git commit -m 'the whole example'
```

produce a git patch file

```
nomad-enterprise $ git diff HEAD~1 HEAD > diff.patch
```

<details><summary>contents of <code>diff.patch</code>:</summary>

```diff
diff --git a/example/example.go b/example/example.go
new file mode 100644
index 000000000..50d9e7167
--- /dev/null
+++ b/example/example.go
@@ -0,0 +1,7 @@
+package example
+
+import "fmt"
+
+func ShowDemo() {
+       fmt.Println(demo())
+}
diff --git a/example/example_ent.go b/example/example_ent.go
new file mode 100644
index 000000000..cc4dc4f1b
--- /dev/null
+++ b/example/example_ent.go
@@ -0,0 +1,7 @@
+// go:build ent
+
+package example
+
+func demo() string {
+       return "hi i'm in ent"
+}
diff --git a/example/example_ce.go b/example/example_ce.go
new file mode 100644
index 000000000..4d54cf945
--- /dev/null
+++ b/example/example_ce.go
@@ -0,0 +1,7 @@
+// go:build !ent
+
+package example
+
+func demo() string {
+       return "hi i'm in ce"
+}
```
</details>

Your may use specific commit sha or shas in your diff,
instead of `HEAD~1` and `HEAD`, whatever makes sense to you.

### Apply the patch

Then, apply the patch in CE, excluding any files that are
only present in the enterprise repo:

```
nomad $ git apply ../nomad-enterprise/diff.patch --exclude '*_ent*'
nomad $ ls example
example.go  example_ce.go
```

Note: you can provide multiple `--exclude` flags with `git apply`.

Also note: `*_ent*` may not be suitable for your particular changes,
and some enterprise files that don't have a corresponding CE version
may not have the `_ent.go` suffix.

Then `git add` the changes as you'd like.
I'm a big fan of `git add --patch` to review the specific changes as you add
them, and make logically separate commits where appropriate, if you'd like. 

Run `git diff --cached` and review the changes to make sure no enterprise
code is present. If it's all good, go ahead and `git commit` the changes.

```
nomad $ git add example
nomad $ git commit -m 'only ce part of the example'
```

If you get conflicts at this point, [God help you](https://git-scm.com/docs).

## Ready for review

Here you have some options

1. Do the whole PR review in ENT, and merge it with the complete changeset.
   Then, apply the partial patch to CE, and open a PR there, almost as a
   formality.
   * The code should be literally identical, so there should be
     no conflicts, but you should validate this by applying the CE patch
     locally before merging ENT.
2. Draft a PR in ENT, so reviewers can see that context, but do the
   review in a CE PR, in public. You'll need to consider whether what
   merges to CE will subsequently pass cleanly through
   [merge CE to ENT](../README-ent.md#updating-enterprise-with-ce-code),
   then merge the ENT PR afterwards.
   * You may need to make placeholder `_ent.go` file(s) or similar so that
     code can compile along the way (see [Gotchas](#Gotchas) below),
     before the ENT PR gets merged.
   * You may also wish to `rebase` your ENT PR on `main` after merging in CE,
     so ENT commit(s) contain only the ENT-specific changes. The rebase/push
     should also re-trigger CI to run tests again, which is nice.
3. Some fun, creative mixture of all this. What an adventure!

What path you choose depends on many factors, such as the size of the
total changeset, the amount of code that is being added to CE vs ENT,
whether public comment is important for these changes, etc.
Use your judgement, and if in doubt, ask your peers.

Either way, if you'll have multiple PRs, and one should be merged before the
other, you might mark the other as a Draft and mention your intentions in the
PR description(s) for reviewer awareness.

And don't forget the changelog in CE!

## Gotchas

In the above example, I actually laid a trap for you, if you follow option 2
above without making a placeholder `_ent.go` file (or similar) that can compile
after merging CE, during CE->ENT merge, before the final ENT PR is merged in.

In `example.go`, `ShowDemo()` runs `demo()`, which is fine in CE
because `example_ce.go` declares `func demo()` so all compiles cleanly.

However, part of the "merge CE to ENT" procedure is to try to build
the program with the `ent` tag. At that time, `example_ent.go`
doesn't exist yet with its `ent` build tag and its own `demo()` func.
It only exists in your ENT PR, which hasn't been merged yet.

That will cause the CE to ENT merge build to fail.

When it does, you can follow the instructions at the bottom of the
GitHub Actions build page to create a PR manually and merge it.
That will leave ENT broken until your PR is merged, assuming that
your PR actually does tie all the loose ends together.
Re-running tests on your PR manually before merging can confirm this.

Happy merging!