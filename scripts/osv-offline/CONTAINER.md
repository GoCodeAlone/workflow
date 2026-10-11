The container runtime replaces the rejected bubblewrap/systemd prerequisite on
the same ephemeral GitHub-hosted Linux/amd64 runner. Jon approved this boundary
and bounded validation on 2026-10-11 at 15:05 UTC. The provider Docker daemon is
trusted; no daemon remapping, host-policy change or persistent runner is used.

The image is an exact official Python Linux/amd64 manifest and config digest.
The nonroot manager and its scanner subprocesses share memory 2 GiB, swap 0,
pids 256 and CPU 2. Network-none, read-only inputs, closed inherited descriptors,
zero capabilities, no-new-privileges and the pinned syscall policy are required.
Fresh user-namespace remapping is not promised. OCI/libseccomp bad-ABI handling
may kill a thread rather than the original filter's entire process.
Both i386 compat and x32 denial are actual runtime gates. The policy preserves
the original denials
and corrects the missing io_uring_setup 425 while retaining open_tree 428.
Proof children use the existing producer's ordinary fork launch path. The
clone3 EPERM denial remains in force; posix_spawn compatibility is not assumed.

The host controller creates and binds the full container ID before starting.
Client timeout does not retire the container. ID-specific kill/wait/removal,
verified absence and the final deadline must precede successful publication.
An uncertain create request cannot credit retirement from a first absence
snapshot. Absence requires an exact-target Docker not-found response and an
empty inspection result. Receipts record creation attempts and inspection
classifications, byte counts and hashes without retaining raw diagnostics.
Every read-only input needs a kernel mount-flag witness, with a real
EROFS write-denial witness on the separately owned fixture.
No host credential, home or socket is mounted. Credentialed reporting remains
outside the worker. Runtime proof does not grant a clean vulnerability result
or permanent installation: complete raw findings and real workflow checks retain
their independent gates, including every advisory-only finding.
