# shellcheck shell=bash
# shellcheck disable=SC2154 # $work and die belong to the script that sources this file
# Sourced by contract.sh (OC-09a3, DEVDESK-944) and by the tests of
# internal/testutil/enginecontract: the strict extraction of the tar a test VM sent back.
#
# The tar is untrusted input. unpack_recorded lists it first and refuses it as a whole when:
#   - it is larger than max_archive bytes;
#   - an entry is not "./", "<campaign>/" or "<campaign>/<name>.cli" (an absolute path, "..", a name
#     with a line break or any other odd character);
#   - an entry is not a regular file or a directory (a symbolic or hard link, a device, a FIFO);
#   - the sizes the listing announces add up to more than max_archive: a member that is larger on
#     its own (a sparse file that expands to gigabytes) is refused by that, and so are many small ones.
# Then it extracts into a new private directory (mktemp -d under $work), with --no-same-owner and
# --no-same-permissions and never -P, and removes group and other permissions. engine-fixture-import
# reads every file again.
#
# Needs: $work (a directory), die (prints a message and exits), GNU tar. Sets $recorded.
max_archive=$((16 * 1024 * 1024))
recorded=""
unpack_recorded() {
  local names listing entry
  # The archive is one file of the work directory; each tar below names it.
  (($(wc -c <"$work/recorded-out") <= max_archive)) || die "the recorded archive is larger than $max_archive bytes: refused"
  names=$(tar -tf "$work/recorded-out") || die "the recorded archive cannot be read"
  listing=$(tar -tvf "$work/recorded-out") || die "the recorded archive cannot be read"
  while IFS= read -r entry; do
    [[ $entry =~ ^(\./?|(\./)?[a-z0-9-]+/?|(\./)?[a-z0-9-]+/[A-Za-z0-9._-]+\.cli)$ ]] ||
      die "the recorded archive holds an entry that is not <campaign>/<name>.cli (absolute path, '..', odd name): refused"
  done <<<"$names"
  local total=0
  while IFS= read -r entry; do
    [[ ${entry:0:1} == - || ${entry:0:1} == d ]] ||
      die "the recorded archive holds a link or a special file: refused"
    [[ $entry =~ ^.[-rwxsStT]{9}[[:space:]]+[^[:space:]]+[[:space:]]+([0-9]+)[[:space:]] ]] ||
      die "the recorded archive has an entry whose size cannot be read: refused"
    total=$((total + BASH_REMATCH[1]))
    ((total <= max_archive)) || die "the recorded archive announces more than $max_archive bytes of files: refused"
  done <<<"$listing"
  recorded=$(mktemp -d "$work/recorded.XXXXXX")
  tar -xf "$work/recorded-out" -C "$recorded" --no-same-owner --no-same-permissions
  chmod -R u+rwX,go-rwx "$recorded"
}
