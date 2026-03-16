BEGIN {
    print "TAP version 14"
    n = 0
    current = ""
    split("", closed)
}

function classify(line) {
    if (line ~ /^Created autostash:/ || line == "Applied autostash." || line ~ /^Dropped refs\/stash/) {
        return "stash"
    }
    if (line ~ /^Current branch / || line ~ /^Updating / || line == "Fast-forward" || line ~ /^Successfully rebased/ || line ~ /^Applying: / || line ~ /^Rebasing \(/ || line ~ /^CONFLICT / || line ~ /^Auto-merging / || line == "Already up to date.") {
        return "rebase"
    }
    if ((line ~ /^ .+\|/) || line ~ /files? changed/ || line ~ /insertion/ || line ~ /deletion/) {
        return "summary"
    }
    return ""
}

function emit_phase() {
    if (current != "") {
        n++
        printf "ok %d - %s\n", n, current
        closed[current] = 1
    }
}

{
    line = $0
    sub(/^[[:space:]]+/, "", line)
    phase = classify(line)

    printf "# %s\n", $0

    if (phase != "" && phase != current && !(phase in closed)) {
        emit_phase()
        current = phase
    }
}

END {
    emit_phase()
    printf "1..%d\n", n
}
