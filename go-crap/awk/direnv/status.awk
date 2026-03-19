BEGIN {
    print "TAP version 14"
    n = 0
    current = ""
    split("", closed)
}

function classify(line,    trimmed) {
    trimmed = line
    sub(/^[[:space:]]+/, "", trimmed)
    if (trimmed ~ /^direnv exec path/ || trimmed ~ /^DIRENV_CONFIG/ || trimmed ~ /^bash_path/ || trimmed ~ /^disable_stdin/ || trimmed ~ /^warn_timeout/ || trimmed ~ /^whitelist\./) {
        return "config"
    }
    if (trimmed ~ /^Loaded RC/ || trimmed ~ /^Loaded watch:/) {
        return "loaded"
    }
    if (trimmed ~ /^Found RC/ || trimmed ~ /^Found watch:/) {
        return "found"
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
