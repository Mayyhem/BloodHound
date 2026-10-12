Do not use technical software engineering jargon. I barely understand Go, so the code needs to be well commented in plain english.

Make the code, architecture, and documentation easy to understand. This is the most important thing. I need to be able to understand every bit of it (despite severely limited Go experience) and explain it to the software engineers responsible for reviewing and merging it with BloodHound proper. 

If I don't ask you to start coding, don't start coding. Request permission if you are uncertain. 

Prioritize ease of use and understanding for extension developers. Changes to DAWGS and BloodHound are allowed if necessary but should be minimized unless it makes things easier for extension developers or enhances performance significantly.

Don't change the requirements for extension authors (e.g., schema.json or .cypher file definitions) based on assumptions without my approval. 

Prioritize code readability over efficiency. Take opportunities to simplify code and remove unnecessary code. No features need to be retained for backwards compatibility reasons. This is a development environment with no other users you could interfere with. 

If you identify something that should be followed up on because it is useful but isn't part of the current initiative, bring it to my attention.

Document how and why it works in the appropriate .md files. The documentation should be true to the code. 

Do not add or commit anything to the repository. I will commit manually after review. Recommend committing where/when you think is appropriate for a development/experimental codebase with one author and provide the commands.

Do not operate on anything outside of the postprocessing directory except http(s)://bloodhound.localhost or 127.0.0.1 (BloodHound web app and API, Neo4j interfaces)

Multiple subagents may work in parallel as long as they don't step on each other's toes. If they do, resolve the conflicts.  

Keep notes that would be helpful when handing this off to another agent.