# 检查 $GIT_HOST $NETRC_MACHINE $NETRC_LOGIN $NETRC_TOKEN 并替换
git config --global url."git@$GIT_HOST:".insteadof https://$GIT_HOST/ && \
git config --global url."ssh://git@$GIT_HOST:".insteadof https://$GIT_HOST/
mkdir -p -m 0600 ~/.ssh && ssh-keyscan $GIT_HOST >> ~/.ssh/known_hosts
touch ~/.netrc && \
    echo "machine $NETRC_MACHINE" >> ~/.netrc && \
    echo "login $NETRC_LOGIN" >> ~/.netrc && \
    echo "password $NETRC_TOKEN" >> ~/.netrc && \
    chmod 0600 ~/.netrc