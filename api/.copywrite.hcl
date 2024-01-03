schema_version = 1

project {
  license        = "MPL-2.0"
  copyright_year = 2024

  header_ignore = [
    // Enterprise files do not fall under the open source licensing. OSS-ENT
    // merge conflicts might happen here, please be sure to put new OSS
    // exceptions above this comment.
    "**/*_ent.go",
    "**/*_ent_test.go",
  ]
}
