# Check a Go coverage profile without rounding the percentage before comparison.
# Duplicate blocks (for example, from cross-package runs) are counted only once;
# a block is covered if any observation executed it. No paths are excluded.
BEGIN {
    if (minimum !~ /^[0-9]+([.][0-9]+)?$/ || minimum > 100) {
        print "coverage: minimum must be a percentage from 0 to 100" > "/dev/stderr"
        invalid = 1
        exit 2
    }
}
NR == 1 {
    if ($0 !~ /^mode: (set|count|atomic)$/) {
        print "coverage: invalid profile header" > "/dev/stderr"
        invalid = 1
        exit 2
    }
    next
}
{
    if (NF != 3 || $1 !~ /:[0-9]+[.][0-9]+,[0-9]+[.][0-9]+$/ ||
        $2 !~ /^[0-9]+$/ || $3 !~ /^[0-9]+$/) {
        print "coverage: invalid block at line " NR > "/dev/stderr"
        invalid = 1
        exit 2
    }
    if ($1 in statements && statements[$1] != $2) {
        print "coverage: inconsistent duplicate block at line " NR > "/dev/stderr"
        invalid = 1
        exit 2
    }
    statements[$1] = $2
    if ($3 > 0) {
        executed[$1] = 1
    }
}
END {
    if (invalid) {
        exit 2
    }
    for (block in statements) {
        total += statements[block]
        if (executed[block]) {
            covered += statements[block]
        }
    }
    if (total == 0) {
        print "coverage: profile contains no statements" > "/dev/stderr"
        exit 2
    }
    printf "Coverage: %.3f%% (%d/%d statements); required: %g%%\n", \
        100 * covered / total, covered, total, minimum
    if (100 * covered < minimum * total) {
        exit 1
    }
}
