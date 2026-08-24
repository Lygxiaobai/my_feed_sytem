---
scenarios:
  - name: comment-tree
    description: A user can publish a root comment and a reply, list them, and delete an owned comment according to the API rules.
    expected: The response preserves root and parent relationships and deletion does not leave an invalid reply tree.
    tags:
      - backend-api
  - name: comment-writes-interest-tags
    description: A signed-in user publishes a comment on a video that already has stored tags.
    expected: After the comment is durable the commenter's interests field contains a tag from that video. A comment on a video with no tags leaves interests unchanged.
    tags:
      - backend-api
