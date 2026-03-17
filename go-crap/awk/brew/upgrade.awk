BEGIN {
    print "TAP version 14"
    pkg_n = 0
    phase_n = 0
    current_phase = ""
    current_pkg = ""
    in_pkg = 0
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

function close_phase() {
    if (current_phase != "") {
        phase_n++
        printf "    ok %d - %s\n", phase_n, current_phase
        closed[current_phase] = 1
    }
}

function close_pkg() {
    if (in_pkg) {
        close_phase()
        printf "    1..%d\n", phase_n
        pkg_n++
        printf "ok %d - %s\n", pkg_n, current_pkg
        phase_n = 0
        current_phase = ""
        split("", closed)
        in_pkg = 0
    }
}

{
    trimmed = $0
    sub(/^[[:space:]]+/, "", trimmed)
}

trimmed ~ /^==> Upgrading / {
    close_pkg()
    current_pkg = trimmed
    sub(/^==> Upgrading /, "", current_pkg)
    in_pkg = 1
    printf "# %s\n", $0
    next
}

{
    if (in_pkg) {
        phase = classify($0)

        printf "    # %s\n", $0

        if (phase != "" && phase != current_phase && !(phase in closed)) {
            close_phase()
            current_phase = phase
        }
    } else {
        printf "# %s\n", $0
    }
}

END {
    close_pkg()
    printf "1..%d\n", pkg_n
}
