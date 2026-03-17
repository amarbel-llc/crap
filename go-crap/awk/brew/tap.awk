BEGIN {
    print "TAP version 14"
    n = 0
    current = ""
    split("", closed)
}

function classify(line,    trimmed) {
    trimmed = line
    sub(/^[[:space:]]+/, "", trimmed)

    if (trimmed ~ /^==> Tapping/ || trimmed ~ /^Cloning into/ || trimmed ~ /^remote: / || trimmed ~ /^Receiving objects:/ || trimmed ~ /^Resolving deltas:/) {
        return "clone"
    }
    if (trimmed ~ /^==> Tapped/) {
        return "install"
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
    phase = classify($0)

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
