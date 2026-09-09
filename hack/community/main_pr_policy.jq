# Feature integration requires a PR and resolved conversations, without a
# human review gate. Independent approval is enforced at release publication.
any(.[];
  .type == "pull_request" and
  .parameters.required_approving_review_count == 0 and
  .parameters.dismiss_stale_reviews_on_push == true and
  .parameters.require_code_owner_review == false and
  .parameters.require_last_push_approval == false and
  .parameters.required_review_thread_resolution == true
)
