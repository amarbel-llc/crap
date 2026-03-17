BEGIN {
    print "TAP version 14"
    n = 0
    current = ""
    split("", closed)
}

function classify(line,    trimmed) {
    trimmed = line
    sub(/^[[:space:]]+/, "", trimmed)

    if (trimmed ~ /^==> Downloading/ || trimmed ~ /^==> Fetching/ || trimmed ~ /^Already downloaded:/) {
        return "download"
    }
    if (trimmed ~ /^==> Installing/ || trimmed ~ /^==> Pouring/) {
        return "install"
    }
    if (trimmed ~ /^==> Linking/) {
        return "link"
    }
    if (trimmed ~ /^==> Caveats/) {
        return "caveats"
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
